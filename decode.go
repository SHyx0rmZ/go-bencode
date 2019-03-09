package bencode

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

func Unmarshal(data []byte, v interface{}) error {
	var d decodeState
	err := checkValid(data, &d.scan)
	if err != nil {
		return err
	}

	d.init(data)
	return d.unmarshal(v)
}

type UnmarshalTypeError struct {
	Value  string
	Type   reflect.Type
	Offset int64
	Struct string
	Field  string
}

func (e *UnmarshalTypeError) Error() string {
	if e.Struct != "" || e.Field != "" {
		return "bencode: cannot unmarshal " + e.Value + " into Go struct field " + e.Struct + "." + e.Field + " of type " + e.Type.String()
	}
	return "bencode: cannot unmarshal " + e.Value + " into Go value of type " + e.Type.String()
}

type InvalidUnmarshalError struct {
	Type reflect.Type
}

func (e *InvalidUnmarshalError) Error() string {
	if e.Type == nil {
		return "bencode: Unmarshal(nil)"
	}

	if e.Type.Kind() != reflect.Ptr {
		return "bencode: Unmarshal(non-pointer " + e.Type.String() + ")"
	}
	return "bencode: Unmarshal(nil " + e.Type.String() + ")"
}

type decodeState struct {
	data         []byte
	off          int
	opcode       int
	scan         scanner
	errorContext struct {
		Struct reflect.Type
		Field  string
	}
	savedError            error
	useNumber             bool
	disallowUnknownFields bool
}

func (d *decodeState) readIndex() int {
	return d.off - 1
}

const phasePanicMsg = "Bencode decoder out of sync - data changing underfoot?"

func (d *decodeState) init(data []byte) *decodeState {
	d.data = data
	d.off = 0
	d.savedError = nil
	d.errorContext.Struct = nil
	d.errorContext.Field = ""
	return d
}

func (d *decodeState) saveError(err error) {
	if d.savedError == nil {
		d.savedError = d.addErrorContext(err)
	}
}

func (d *decodeState) addErrorContext(err error) error {
	if d.errorContext.Struct != nil || d.errorContext.Field != "" {
		switch err := err.(type) {
		case *UnmarshalTypeError:
			err.Struct = d.errorContext.Struct.Name()
			err.Field = d.errorContext.Field
			return err
		}
	}
	return err
}

func (d *decodeState) skip() {
	s, data, i := &d.scan, d.data, d.off
	depth := len(s.parseState)
	for {
		op := s.step(s, data[i])
		i++
		if len(s.parseState) < depth {
			d.off = i
			d.opcode = op
			return
		}
	}
}

func (d *decodeState) scanNext() {
	if d.off < len(d.data) {
		d.scan.bytes++
		d.opcode = d.scan.step(&d.scan, d.data[d.off])
		d.off++
	} else {
		d.opcode = d.scan.eof()
		d.off = len(d.data) + 1
	}
}

func (d *decodeState) scanWhile(op int) {
	s, data, i := &d.scan, d.data, d.off
	for i < len(data) {
		newOp := s.step(s, data[i])
		i++
		if newOp != op {
			d.opcode = newOp
			d.off = i
			return
		}
	}

	d.off = len(data) + 1
	d.opcode = d.scan.eof()
}

func (d *decodeState) unmarshal(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return &InvalidUnmarshalError{reflect.TypeOf(v)}
	}

	d.scan.reset()
	d.scanNext()
	if d.scan.bytes == 0 {
		return io.EOF
	}
	err := d.value(rv)
	if err != nil {
		return d.addErrorContext(err)
	}
	return d.savedError
}

func (d *decodeState) value(v reflect.Value) error {
	switch d.opcode {
	default:
		panic(phasePanicMsg)

	case scanBeginDictionary:
		if v.IsValid() {
			if err := d.dictionary(v); err != nil {
				return err
			}
		} else {
			d.skip()
		}
		d.scanNext()

	case scanBeginList:
		if v.IsValid() {
			if err := d.list(v); err != nil {
				return err
			}
		} else {
			d.skip()
		}
		d.scanNext()
		//d.scanNext()

	case scanBeginInteger:
		d.scanNext()
		if d.opcode != scanInteger {
			panic(phasePanicMsg)
		}

		start := d.readIndex()
		d.scanWhile(scanContinue)

		if v.IsValid() {
			if err := d.integerStore(d.data[start:d.readIndex()], v, false); err != nil {
				return err
			}
		}
		d.scanNext()

	case scanBeginString:
		d.scanWhile(scanContinue)
		if d.opcode != scanString {
			panic(phasePanicMsg)
		}

		start := d.readIndex()
		d.scanWhile(scanContinue)

		if v.IsValid() {
			if err := d.stringStore(d.data[start:d.readIndex()], v, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func indirect(v reflect.Value, decodingNull bool) reflect.Value {
	v0 := v
	haveAddr := false

	if v.Kind() != reflect.Ptr && v.Type().Name() != "" && v.CanAddr() {
		haveAddr = true
		v = v.Addr()
	}
	for {
		if v.Kind() == reflect.Interface && !v.IsNil() {
			e := v.Elem()
			if e.Kind() == reflect.Ptr && !e.IsNil() && (!decodingNull || e.Elem().Kind() == reflect.Ptr) {
				haveAddr = false
				v = e
				continue
			}
		}

		if v.Kind() != reflect.Ptr {
			break
		}

		if v.Elem().Kind() != reflect.Ptr && decodingNull && v.CanSet() {
			break
		}
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		if v.Type().NumMethod() > 0 && v.CanInterface() {
			// interfaces (Unarmasher)
		}

		if haveAddr {
			v = v0
			haveAddr = false
		} else {
			v = v.Elem()
		}
	}
	return v
}

func (d *decodeState) list(v reflect.Value) error {
	v = indirect(v, false)

	switch v.Kind() {
	case reflect.Interface:
		panic("interface")
	default:
		d.saveError(&UnmarshalTypeError{Value: "list", Type: v.Type(), Offset: int64(d.off)})
		d.skip()
		return nil
	case reflect.Array, reflect.Slice:
		break
	}

	i := 0
	d.scanNext()
	for {
		if d.opcode == scanEndList {
			break
		}
		if d.opcode != scanBeginInteger && d.opcode != scanBeginList && d.opcode != scanBeginDictionary && d.opcode != scanBeginString { // todo
			panic(phasePanicMsg)
		}

		if v.Kind() == reflect.Slice {
			if i >= v.Cap() {
				newcap := v.Cap() + v.Cap()/2
				if newcap < 4 {
					newcap = 4
				}
				newv := reflect.MakeSlice(v.Type(), v.Len(), newcap)
				reflect.Copy(newv, v)
				v.Set(newv)
			}
			if i >= v.Len() {
				v.SetLen(i + 1)
			}
		}

		if i < v.Len() {
			if err := d.value(v.Index(i)); err != nil {
				return err
			}
		} else {
			if err := d.value(reflect.Value{}); err != nil {
				return err
			}
		}
		i++
	}

	if i < v.Len() {
		if v.Kind() == reflect.Array {
			z := reflect.Zero(v.Type().Elem())
			for ; i < v.Len(); i++ {
				v.Index(i).Set(z)
			}
		} else {
			v.SetLen(i)
		}
	}
	if i == 0 && v.Kind() == reflect.Slice {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	}
	return nil
}

func (d *decodeState) dictionary(v reflect.Value) error {
	v = indirect(v, false)
	t := v.Type()

	if v.Kind() == reflect.Interface && v.NumMethod() == 0 {
		//
	}

	var fields []field

	switch v.Kind() {
	case reflect.Map:
		switch t.Key().Kind() {
		case reflect.String:
		default:
			d.saveError(&UnmarshalTypeError{Value: "dictionary", Type: t, Offset: int64(d.off)})
			d.skip()
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(t))
		}
	case reflect.Struct:
		fields = cachedTypeFields(t)
		// ok
	default:
		d.saveError(&UnmarshalTypeError{Value: "dictionary", Type: t, Offset: int64(d.off)})
		d.skip()
		return nil
	}

	var mapElem reflect.Value
	originalErrorContext := d.errorContext

	d.scanWhile(scanContinue)
	for {
		//d.scanWhile(scanContinue)
		if d.opcode == scanEndDictionary {
			break
		}
		if d.opcode != scanBeginString {
			panic(phasePanicMsg)
		}
		d.scanWhile(scanContinue)
		if d.opcode != scanString {
			panic(phasePanicMsg)
		}

		start := d.readIndex()
		d.scanWhile(scanContinue)
		key := d.data[start:d.readIndex()]

		var subv reflect.Value
		destring := false

		if v.Kind() == reflect.Map {
			elemType := t.Elem()
			if !mapElem.IsValid() {
				mapElem = reflect.New(elemType).Elem()
			} else {
				mapElem.Set(reflect.Zero(elemType))
			}
			subv = mapElem
		} else {
			var f *field
			for i := range fields {
				ff := &fields[i]
				if bytes.Equal(ff.nameBytes, key) {
					f = ff
					break
				}
				if f == nil && ff.equalFold(ff.nameBytes, key) {
					f = ff
				}
			}
			if f != nil {
				subv = v
				destring = f.quoted
				for _, i := range f.index {
					if subv.Kind() == reflect.Ptr {
						if subv.IsNil() {
							if !subv.CanSet() {
								d.saveError(fmt.Errorf("bencode: cannot set embedded pointer to unexported struct: %v", subv.Type().Elem()))
								subv = reflect.Value{}
								destring = false
								break
							}
							subv.Set(reflect.New(subv.Type().Elem()))
						}
						subv = subv.Elem()
					}
					subv = subv.Field(i)
				}
				d.errorContext.Field = f.name
				d.errorContext.Struct = t
			} else if d.disallowUnknownFields {
				d.saveError(fmt.Errorf("bencode: unknown field %q", key))
			}
		}

		//if d.opcode != scanDictionaryKey {
		//	panic(phasePanicMsg)
		//}

		if destring {
			panic("not implemented")
		} else {
			if err := d.value(subv); err != nil {
				return err
			}
		}

		if v.Kind() == reflect.Map {
			kt := t.Key()
			var kv reflect.Value
			switch {
			case kt.Kind() == reflect.String:
				kv = reflect.ValueOf(key).Convert(kt)
			//case interface
			default:
				panic("bencode: unexpected key type")
			}
			if kv.IsValid() {
				v.SetMapIndex(kv, subv)
			}
		}

		if d.opcode == scanEndDictionary {
			break
		}

		d.errorContext = originalErrorContext
	}
	return nil
}

func (d *decodeState) convertNumber(s string) (interface{}, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, &UnmarshalTypeError{Value: "number " + s, Type: reflect.TypeOf(0.0), Offset: int64(d.off)}
	}
	return f, nil
}

func (d *decodeState) integerStore(item []byte, v reflect.Value, fromQuoted bool) error {
	if len(item) == 0 {
		d.saveError(fmt.Errorf("bencode: invalid use of ,string struct tag, trying to unmarshal %q into %v", item, v.Type()))
		return nil
	}

	v = indirect(v, false)

	c := item[0]
	if c != '-' && (c < '0' || c > '9') {
		if fromQuoted {
			return fmt.Errorf("bencode: invalid use of ,string struct tag, trying to unmarshal %q into %v", item, v.Type())
		}
		panic(phasePanicMsg)
	}
	s := string(item)
	switch v.Kind() {
	default:
		if fromQuoted {
			return fmt.Errorf("bencode: invalid use of ,string struct tag, trying to unmarshal %q into %v", item, v.Type())
		}
		d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.readIndex())})
	case reflect.Interface:
		n, err := d.convertNumber(s)
		if err != nil {
			d.saveError(err)
			break
		}
		if v.NumMethod() != 0 {
			d.saveError(&UnmarshalTypeError{Value: "number", Type: v.Type(), Offset: int64(d.readIndex())})
			break
		}
		v.Set(reflect.ValueOf(n))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v.OverflowInt(n) {
			d.saveError(&UnmarshalTypeError{Value: "number " + s, Type: v.Type(), Offset: int64(d.readIndex())})
			break
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint64, reflect.Uintptr:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil || v.OverflowUint(n) {
			d.saveError(&UnmarshalTypeError{Value: "number " + s, Type: v.Type(), Offset: int64(d.readIndex())})
			break
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(s, v.Type().Bits())
		if err != nil || v.OverflowFloat(n) {
			d.saveError(&UnmarshalTypeError{Value: "number " + s, Type: v.Type(), Offset: int64(d.readIndex())})
			break
		}
		v.SetFloat(n)
	}
	return nil
}

func (d *decodeState) stringStore(item []byte, v reflect.Value, fromQuoted bool) error {
	if len(item) == 0 {
		d.saveError(fmt.Errorf("bencode: invalid use of ,string struct tag, trying to unmarshal %q into %v", item, v.Type()))
		return nil
	}

	v = indirect(v, false)

	s := string(item)
	switch v.Kind() {
	default:
		d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.readIndex())})
	case reflect.Slice:
		if v.Type().Elem().Kind() != reflect.Uint8 {
			d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.readIndex())})
			break
		}
		b := make([]byte, base64.StdEncoding.DecodedLen(len(s)))
		n, err := base64.StdEncoding.Decode(b, []byte(s))
		if err != nil {
			d.saveError(err)
			break
		}
		v.SetBytes(b[:n])
	case reflect.String:
		v.SetString(string(s))
	case reflect.Interface:
		if v.NumMethod() == 0 {
			v.Set(reflect.ValueOf(string(s)))
		} else {
			d.saveError(&UnmarshalTypeError{Value: "string", Type: v.Type(), Offset: int64(d.readIndex())})
		}
	}
	return nil
}
