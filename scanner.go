package bencode

import (
	"strconv"
)

func Valid(data []byte) bool {
	return checkValid(data, &scanner{}) == nil
}

func checkValid(data []byte, scan *scanner) error {
	scan.reset()
	for _, c := range data {
		scan.bytes++
		//s := scan.step(scan, c)
		//fmt.Println(scanToken(s))
		//if s == scanError {
		if scan.step(scan, c) == scanError {
			return scan.err
		}
	}
	if scan.eof() == scanError {
		return scan.err
	}
	return nil
}

type SyntaxError struct {
	msg    string
	Offset int64
}

func (e *SyntaxError) Error() string { return e.msg }

const (
	scanContinue = iota

	scanBeginDictionary
	scanEndDictionary

	scanBeginList
	scanEndList

	scanBeginInteger
	scanInteger
	scanEndInteger

	scanBeginString
	scanString

	scanEnd
	scanError
)

const (
	parseDictionaryKey = iota
	parseDictionaryValue

	parseListValue

	parseInteger

	parseStringLength
	parseString
)

type scanner struct {
	step func(*scanner, byte) int

	parseState []int

	endTop bool

	err error

	bytes int64

	string uint64

	digits []byte
}

func (s *scanner) reset() {
	s.step = sv
	s.parseState = s.parseState[0:0]
	s.err = nil
	s.endTop = false
	s.digits = s.digits[0:0]
}

func (s *scanner) error(c byte, context string) int {
	s.step = stateError
	s.err = &SyntaxError{"invalid character " + quoteChar(c) + " " + context, s.bytes}
	return scanError
}

func quoteChar(c byte) string {
	if c == '\'' {
		return `'\''`
	}
	if c == '"' {
		return `'"'`
	}
	s := strconv.Quote(string(c))
	return "'" + s[1:len(s)-1] + "'"
}

func stateError(s *scanner, c byte) int {
	return scanError
}

func (s *scanner) eof() int {
	if s.err != nil {
		return scanError
	}
	if s.endTop {
		return scanEnd
	}
	if s.err == nil {
		s.err = &SyntaxError{"unexpected end of Bencode input", s.bytes}
	}
	return scanError
}

func (s *scanner) pushParseState(p int) {
	s.parseState = append(s.parseState, p)
}

func (s *scanner) popParseState() {
	n := len(s.parseState) - 1
	s.parseState = s.parseState[0:n]
	if n == 0 {
		s.step = stateEndTop
		s.endTop = true
	} else {
		s.step = se
	}
}

func stateEndTop(s *scanner, c byte) int {
	return scanEnd
}

func sv(s *scanner, c byte) int {
	switch c {
	case 'd':
		s.step = sd
		s.pushParseState(parseDictionaryKey)
		return scanBeginDictionary
	case 'l':
		s.step = sl
		s.pushParseState(parseListValue)
		return scanBeginList
	case 'i':
		s.step = si
		s.pushParseState(parseInteger)
		return scanBeginInteger
	case '0':
		s.step = ssl0
		s.pushParseState(parseStringLength)
		s.digits = append(s.digits, c)
		return scanBeginString
	}
	if '1' <= c && c <= '9' {
		s.step = ssl
		s.pushParseState(parseStringLength)
		s.digits = append(s.digits, c)
		return scanBeginString
	}
	return s.error(c, "looking for value")
}

func sd(s *scanner, c byte) int {
	switch c {
	case 'e':
		s.popParseState()
		return scanEndDictionary
	case '0':
		s.step = ssl0
		s.pushParseState(parseStringLength)
		s.digits = append(s.digits, c)
		return scanBeginString
	}
	if '1' <= c && c <= '9' {
		s.step = ssl
		s.pushParseState(parseStringLength)
		s.digits = append(s.digits, c)
		return scanBeginString
	}
	return s.error(c, "looking for string length")
}

func ssl0(s *scanner, c byte) int {
	if c == ':' {
		return ssle(s, c)
	}
	return s.error(c, "looking for string length delimiter")
}

func ssl(s *scanner, c byte) int {
	if c == ':' {
		return ssle(s, c)
	}
	if '0' <= c && c <= '9' {
		s.digits = append(s.digits, c)
		return scanContinue
	}
	return s.error(c, "looking for string length digit")
}

func ssle(s *scanner, c byte) int {
	if s.string == 0 {
		n, err := strconv.ParseUint(string(s.digits), 10, 64)
		s.digits = s.digits[0:0]
		if err != nil {
			s.err = err
			s.step = stateError
			return scanError
		}
		s.string = n
		s.parseState[len(s.parseState)-1] = parseString
		s.step = ssf
		if n == 0 {
			s.popParseState()
			return scanString
		}
		return scanContinue
	} else {
		panic(phasePanicMsg)
	}
}

func ssf(s *scanner, c byte) int {
	s.string--
	if s.string == 0 {
		s.popParseState()
		return scanString
	}
	s.step = ss
	return scanString
}

func ss(s *scanner, c byte) int {
	s.string--
	if s.string == 0 {
		s.popParseState()
		return scanContinue
	}
	return scanContinue
}

func se(s *scanner, c byte) int {
	n := len(s.parseState)
	if n == 0 {
		s.step = stateEndTop
		s.endTop = true
		return scanEnd
	}
	ps := s.parseState[n-1]
	switch ps {
	case parseString:
		panic(phasePanicMsg)
	case parseDictionaryKey:
		s.parseState[n-1] = parseDictionaryValue
		return sv(s, c)
	case parseDictionaryValue:
		s.parseState[n-1] = parseDictionaryKey
		return sd(s, c)
	case parseListValue:
		return sl(s, c)
	}
	return s.error(c, "DNE")
}

func sl(s *scanner, c byte) int {
	if c == 'e' {
		s.popParseState()
		return scanEndList
	}
	return sv(s, c)
}

func si(s *scanner, c byte) int {
	switch c {
	case '-':
		s.step = sin
		return scanInteger
	case '0':
		s.step = si0
		return scanInteger
	}
	if '1' <= c && c <= '9' {
		s.step = sil
		return scanInteger
	}
	return s.error(c, "looking for integer")
}

func sin(s *scanner, c byte) int {
	if c == '0' {
		return s.error(c, "negative zero not allowed")
	}
	if '1' <= c && c <= '9' {
		s.step = sil
		return scanContinue
	}
	return s.error(c, "looking for integer")
}

func si0(s *scanner, c byte) int {
	if c == 'e' {
		s.popParseState()
		return scanEndInteger
	}
	return s.error(c, "leading zeroes not allowed")
}

func sil(s *scanner, c byte) int {
	if c == 'e' {
		s.popParseState()
		return scanEndInteger
	}
	if '0' <= c && c <= '9' {
		return scanContinue
	}
	return s.error(c, "looking for integer")
}
