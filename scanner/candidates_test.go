package scanner

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func typesOf(values []*Value) (types []Type) {
	for _, v := range values {
		types = append(types, v.Type())
	}
	return
}

func TestParseCandidatesTypes(t *testing.T) {
	all := []Type{Int8, Int16, Int32, Int64, Float32, Float64}
	tests := []struct {
		input string
		want  []Type
	}{
		// positive integers: unsigned range of each type, exact floats
		{"100", all},
		{"255", all},
		{"256", []Type{Int16, Int32, Int64, Float32, Float64}},
		{"65535", []Type{Int16, Int32, Int64, Float32, Float64}},
		{"65536", []Type{Int32, Int64, Float32, Float64}},
		{"16777216", []Type{Int32, Int64, Float32, Float64}}, // 2^24: last exact float32
		{"16777217", []Type{Int32, Int64, Float64}},          // 2^24+1: not a float32
		{"4294967295", []Type{Int32, Int64, Float64}},        // max uint32
		{"4294967296", []Type{Int64, Float64}},               //
		{"9007199254740992", []Type{Int64, Float64}},         // 2^53: last exact float64
		{"9007199254740993", []Type{Int64}},                  //
		{"18446744073709551615", []Type{Int64}},              // max uint64
		{"18446744073709551616", nil},                        // overflows everything
		{"+5", all},                                          //
		{"007", all},                                         // leading zeros
		{"  42  ", all},                                      // surrounding blanks
		// negative integers: signed range
		{"-1", all},
		{"-128", all},
		{"-129", []Type{Int16, Int32, Int64, Float32, Float64}},
		{"-32769", []Type{Int32, Int64, Float32, Float64}},
		{"-2147483649", []Type{Int64, Float64}},
		{"-9223372036854775808", []Type{Int64}},
		{"-9223372036854775809", nil},
		// decimals
		{"3.5", []Type{Float32, Float64}},
		{"-3.5", []Type{Float32, Float64}},
		{".5", []Type{Float32, Float64}},
		{"5.", []Type{Float32, Float64}},
		{"1e3", []Type{Float32, Float64}},
		{"1E-3", []Type{Float32, Float64}},
		{"1e50", []Type{Float64}},  // overflows a float32
		{"1e-50", []Type{Float64}}, // would become 0 in a float32
		{"1e400", nil},             // overflows a float64
		// not numbers
		{"", nil},
		{"   ", nil},
		{"abc", nil},
		{"12abc", nil},
		{"1,5", nil},
		{"0x10", nil},
		{"0x1p-2", nil},
		{"1_000", nil},
		{"NaN", nil},
		{"inf", nil},
		{"-Inf", nil},
		{"--5", nil},
		{"+-5", nil},
		{"1-5", nil},
		{"1.2.3", nil},
		{"e", nil},
		{".", nil},
		{"-", nil},
		// zero is refused when allowZero is false (checked below)
	}
	for _, tc := range tests {
		got := typesOf(ParseCandidates(tc.input, OptionFloatUnrounded, false))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: types %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseCandidatesZero(t *testing.T) {
	all := []Type{Int8, Int16, Int32, Int64, Float32, Float64}
	for _, input := range []string{"0", "000", "-0", "+0"} {
		if got := ParseCandidates(input, OptionFloatUnrounded, false); got != nil {
			t.Errorf("%q: %v accepted although zero is refused", input, typesOf(got))
		}
		if got := typesOf(ParseCandidates(input, OptionFloatUnrounded, true)); !reflect.DeepEqual(got, all) {
			t.Errorf("%q with allowZero: %v, want every type", input, got)
		}
	}
	for _, input := range []string{"0.0", "0e5", "-0.0"} {
		if got := ParseCandidates(input, OptionFloatUnrounded, false); got != nil {
			t.Errorf("%q: accepted although zero is refused", input)
		}
		if got := typesOf(ParseCandidates(input, OptionFloatUnrounded, true)); !reflect.DeepEqual(got, []Type{Float32, Float64}) {
			t.Errorf("%q with allowZero: %v, want the float types", input, got)
		}
	}
}

func TestParseCandidatesBytes(t *testing.T) {
	bytesOf := func(input string, typ Type) string {
		v := ParseAs(input, typ, OptionFloatUnrounded, false)
		if v == nil {
			t.Fatalf("%q: no %v candidate", input, typ)
		}
		return hex.EncodeToString(v.Bytes())
	}
	tests := []struct {
		input string
		typ   Type
		want  string // little endian
	}{
		{"100", Int8, "64"},
		{"200", Int8, "c8"}, // unsigned range: the byte a person means
		{"1000", Int16, "e803"},
		{"50000", Int16, "50c3"},
		{"3000000000", Int32, "00 5e d0 b2"},
		{"18446744073709551615", Int64, "ffffffffffffffff"},
		{"-1", Int8, "ff"},
		{"-2", Int32, "feffffff"},
		{"-129", Int16, "7fff"},
		{"3.5", Float32, "00006040"},
		{"1000", Float32, "00007a44"},
		{"1000", Float64, "000000000040 8f40"},
	}
	for _, tc := range tests {
		want := ""
		for _, c := range tc.want {
			if c != ' ' {
				want += string(c)
			}
		}
		if got := bytesOf(tc.input, tc.typ); got != want {
			t.Errorf("%q as %v: bytes %s, want %s", tc.input, tc.typ, got, want)
		}
	}
}

func TestParseCandidatesDisplay(t *testing.T) {
	display := func(input string, typ Type) string {
		v := ParseAs(input, typ, OptionFloatUnrounded, false)
		if v == nil {
			t.Fatalf("%q: no %v candidate", input, typ)
		}
		return v.Format()
	}
	tests := []struct {
		input string
		typ   Type
		want  string
	}{
		{"200", Int8, "200"}, // not -56
		{"50000", Int16, "50000"},
		{"3000000000", Int32, "3000000000"},
		{"18446744073709551615", Int64, "18446744073709551615"},
		{"-56", Int8, "-56"},
		{"-1", Int32, "-1"},
		{"100", Int32, "100"},
		{"3.5", Float32, "3.5"},
		{"1000", Float64, "1000"},
	}
	for _, tc := range tests {
		if got := display(tc.input, tc.typ); got != tc.want {
			t.Errorf("%q as %v is displayed %q, want %q", tc.input, tc.typ, got, tc.want)
		}
	}

	// the display flag does not change the value itself
	v := ParseAs("200", Int8, OptionFloatUnrounded, false)
	if !v.Unsigned() {
		t.Error("a positive integer must be flagged unsigned")
	}
	if raw := v.ToRaw(Int8); raw != int8(-56) {
		t.Errorf("ToRaw gives %v", raw)
	}
	if ParseAs("-56", Int8, OptionFloatUnrounded, false).Unsigned() {
		t.Error("a negative integer must not be flagged unsigned")
	}

	// the same bytes read back by a Value with the same flag
	read := &Value{}
	read.SetType(Int8)
	read.SetBytes([]byte{0xFF})
	if got := read.Format(Int8); got != "-1" {
		t.Errorf("signed display of 0xFF: %q", got)
	}
	read.SetUnsigned(true)
	if got := read.Format(Int8); got != "255" {
		t.Errorf("unsigned display of 0xFF: %q", got)
	}
}

func TestParseCandidatesOption(t *testing.T) {
	for _, option := range []Option{OptionFloatRounded, OptionFloatExtreme, OptionFloatTruncated} {
		for _, v := range ParseCandidates("3", option, false) {
			isFloat := v.Type() == Float32 || v.Type() == Float64
			if v.HasOption() != isFloat {
				t.Errorf("option %v on %v: HasOption = %v", option, v.Type(), v.HasOption())
			}
			if isFloat && v.Option() != option {
				t.Errorf("option %v on %v: got %v", option, v.Type(), v.Option())
			}
		}
	}
	for _, v := range ParseCandidates("3.7", OptionFloatUnrounded, false) {
		if v.HasOption() {
			t.Errorf("%v: unexpected option", v.Type())
		}
	}
}

func TestParseAs(t *testing.T) {
	if v := ParseAs("300", Int8, OptionFloatUnrounded, false); v != nil {
		t.Errorf("300 as Int8: %v", v)
	}
	if v := ParseAs("300", Int16, OptionFloatUnrounded, false); v == nil || v.Type() != Int16 {
		t.Errorf("300 as Int16: %v", v)
	}
	if v := ParseAs("3.5", Int32, OptionFloatUnrounded, false); v != nil {
		t.Errorf("3.5 as Int32: %v", v)
	}
	if v := ParseAs("12", Bytes, OptionFloatUnrounded, false); v != nil {
		t.Errorf("12 as Bytes: %v", v)
	}
}
