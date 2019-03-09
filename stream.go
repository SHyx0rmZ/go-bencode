package bencode

import (
	"io"
)

type Decoder struct {
	r       io.Reader
	buf     []byte
	d       decodeState
	scanp   int
	scanned int64
	scan    scanner
	err     error

	tokenState int
	tokenStack []int
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func (dec *Decoder) Decode(v interface{}) error {
	if dec.err != nil {
		return dec.err
	}

	if err := dec.tokenPrepareForDecode(); err != nil {
		return err
	}

	if !dec.tokenValueAllowed() {
		return &SyntaxError{msg: "not at beginning of value", Offset: dec.offset()}
	}

	n, err := dec.readValue()
	if err != nil {
		return err
	}
	dec.d.init(dec.buf[dec.scanp : dec.scanp+n])
	dec.scanp += n

	err = dec.d.unmarshal(v)

	dec.tokenValueEnd()

	return err
}

func (dec *Decoder) readValue() (int, error) {
	dec.scan.reset()

	scanp := dec.scanp
	var err error
Input:
	for {
		for i, c := range dec.buf[scanp:] {
			dec.scan.bytes++
			switch dec.scan.step(&dec.scan, c) {
			case scanEnd:
				scanp += i
				break Input
			//case scanEndDictionary, scanEndList, scanEndInteger:
			//	if se(&dec.scan, ' ') == scanEnd {
			//		scanp += i + 1
			//		break Input
			//	}
			case scanError:
				dec.err = dec.scan.err
				return 0, dec.scan.err
			}
			if len(dec.scan.parseState) == 0 {
				scanp += i + 1
				break Input
			}
		}
		scanp = len(dec.buf)

		if err != nil {
			if err == io.EOF {
				if dec.scan.bytes == 0 {
					break Input
				}
				err = io.ErrUnexpectedEOF
			}
			dec.err = err
			return 0, err
		}

		n := scanp - dec.scanp
		err = dec.refill()
		scanp = dec.scanp + n
	}
	return scanp - dec.scanp, nil
}

func (dec *Decoder) refill() error {
	if dec.scanp > 0 {
		dec.scanned += int64(dec.scanp)
		n := copy(dec.buf, dec.buf[dec.scanp:])
		dec.buf = dec.buf[:n]
		dec.scanp = 0
	}

	const minRead = 512
	if cap(dec.buf)-len(dec.buf) < minRead {
		newBuf := make([]byte, len(dec.buf), 2*cap(dec.buf)+minRead)
		copy(newBuf, dec.buf)
		dec.buf = newBuf
	}

	n, err := dec.r.Read(dec.buf[len(dec.buf):cap(dec.buf)])
	dec.buf = dec.buf[0 : len(dec.buf)+n]

	return err
}

type Token interface{}

const (
	tokenTopValue = iota
	tokenDictStart
	tokenDictKey
	tokenDictValue
	tokenDictEnd
	tokenListStart
	tokenListValue
	tokenListEnd
)

func (dec *Decoder) tokenPrepareForDecode() error {
	return nil
}

func (dec *Decoder) tokenValueAllowed() bool {
	switch dec.tokenState {
	case tokenTopValue:
		return true
	}
	return false
}

func (dec *Decoder) tokenValueEnd() {
	switch dec.tokenState {
	}
}

func (dec *Decoder) offset() int64 {
	return dec.scanned + int64(dec.scanp)
}
