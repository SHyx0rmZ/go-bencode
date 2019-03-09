package bencode

import (
	"testing"
)

var validTests = []struct {
	data string
	ok   bool
}{
	{`d`, false},
	{`e`, false},
	{`foo`, false},
	{`l`, false},
	{`i`, false},
	{`:`, false},
	{`de`, true},
	{`le`, true},
	{`ie`, false},
	{`i0e`, true},
	{`i-0e`, false},
	{`i1e`, true},
	{`i-1e`, true},
	{`i01e`, false},
	{`i-01e`, false},
	{`0:`, true},
	{`1:`, false},
	{`1:a`, true},
	{`3:foo`, true},
	{`d3:fooi0ee`, true},
	{`d3:fooe`, false},
	{`d0:e`, false},
	{`d1:e`, false},
	{`d1:ae`, false},
	{`d0:0:e`, true},
	{`d1:0:e`, false},
	{`d1:a0:e`, true},
	{`di0ee`, false},
	{`li0ee`, true},
	{`li0ei0ee`, true},
	{`d0:lee`, true},
	{`d1:a0:1:b0:e`, true},
	{`d1:ale1:b0:e`, true},
	{`d1:ali0ee1:b0:e`, true},
	{`d0:d0:deee`, true},
	{`llleee`, true},
	{`ldee`, true},
	{`ld0:0:e0:e`, true},
}

func TestValid(t *testing.T) {
	for _, tt := range validTests {
		if ok := Valid([]byte(tt.data)); ok != tt.ok {
			t.Errorf("Valid(%#q) = %v, want %v", tt.data, ok, tt.ok)
		}
	}
}
