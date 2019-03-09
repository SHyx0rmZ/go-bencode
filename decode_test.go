package bencode

import (
	"testing"
)

func TestUnmarshal(t *testing.T) {
	var data struct {
		Foo string
		Baz []int `bencode:"bar"`
		Int uint8
	}

	err := Unmarshal([]byte(`d3:barli1ei2ei3ee3:foo13:Hello, world!3:Inti42ee`), &data)
	if err != nil {
		t.Error(err)
	}

	if data.Foo != "Hello, world!" {
		t.Error("foo")
	}
	if len(data.Baz) != 3 {
		t.Error("Bar")
	}
	if data.Int != 42 {
		t.Error("Int")
	}
}
