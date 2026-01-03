package scanner

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

var byteOrder = binary.LittleEndian

type Option uint8

const (
	OptionFloatUnrounded Option = iota

	// OptionFloatRounded Round
	// (floor(x + 0.5) == Target
	OptionFloatRounded

	// OptionFloatExtreme Floor | Ceil
	// (floor(x) == Target) OR (ceil(x) == Target)
	OptionFloatExtreme

	// OptionFloatTruncated Floor
	// int(x) == Target
	OptionFloatTruncated
)

func (opt Option) String() string {
	switch opt {
	case OptionFloatRounded:
		return "rounded"
	case OptionFloatExtreme:
		return "extreme"
	case OptionFloatTruncated:
		return "truncated"
	}
	return ""
}

type Value struct {
	typ    Type
	data   []byte
	option Option
}

// Aligned Whether the memory is aligned
// when it is a regular type, memory should always be aligned
func (v *Value) Aligned() bool {
	return v.typ != Bytes
}

func (v *Value) Type() Type {
	return v.typ
}

func (v *Value) Option() Option {
	return v.option
}

func (v *Value) Comparable() ValueComparable {
	if v.HasOption() {
		switch v.Type() {
		case Float32:
			return newRoundedFloat32(*v)
		case Float64:
			return newRoundedFloat64(*v)
		}
	}
	return v
}

func (v *Value) Bytes() []byte {
	return v.data
}

func (v *Value) String() string {
	return fmt.Sprintf("% 02X", v.data)
}

func (v *Value) Size() int {
	if v.typ == Bytes {
		return len(v.data)
	}
	return v.typ.ByteSize()
}

func (v *Value) SetBytes(data []byte) {
	copy(v.data, data)
}

func (v *Value) SetType(typ Type) {
	n := typ.ByteSize()
	v.typ = typ
	v.data = make([]byte, n)
}

func (v *Value) FromString(s string) (err error) {
	bs := v.typ.BitSize()
	switch v.typ {
	case Int8, Int16, Int32, Int64:
		var i int64
		i, err = strconv.ParseInt(s, 10, bs)
		if err != nil {
			return
		}
		switch v.typ {
		case Int8:
			v.data[0] = byte(i)
		case Int16:
			byteOrder.PutUint16(v.data, uint16(i))
		case Int32:
			byteOrder.PutUint32(v.data, uint32(i))
		case Int64:
			byteOrder.PutUint64(v.data, uint64(i))
		default:
		}
	case Float32, Float64:
		var i float64
		i, err = strconv.ParseFloat(s, bs)
		if err != nil {
			return
		}
		switch v.typ {
		case Float32:
			byteOrder.PutUint32(v.data, math.Float32bits(float32(i)))
		case Float64:
			byteOrder.PutUint64(v.data, math.Float64bits(i))
		default:
		}
	case Bytes:
		s = strings.ToLower(strings.Join(strings.Fields(s), ""))
		if len(s)%2 != 0 {
			return fmt.Errorf("invalid hex string: %q", s)
		}
		v.data, err = hex.DecodeString(s)
	}
	return
}

func (v *Value) ToRaw(typ Type) (i any) {
	size := typ.ByteSize()
	if size == 0 {
		size = v.Size()
	}
	b := make([]byte, size)
	copy(b, v.data)

	switch typ {
	case Int8:
		i = int8(b[0])
	case Int16:
		i = int16(byteOrder.Uint16(b))
	case Int32:
		i = int32(byteOrder.Uint32(b))
	case Int64:
		i = int64(byteOrder.Uint64(b))
	case Float32:
		i = math.Float32frombits(byteOrder.Uint32(v.data))
	case Float64:
		i = math.Float64frombits(byteOrder.Uint64(v.data))
	default:
		i = b
	}
	return
}

func (v *Value) Format(args ...Type) string {
	typ := v.typ
	if len(args) > 0 {
		typ = args[0]
	}

	if typ == Bytes {
		return v.String()
	}

	return fmt.Sprint(v.ToRaw(typ))
}

func (v *Value) HasOption() bool {
	if v.Type() == Float32 || v.Type() == Float64 {
		return v.option > OptionFloatUnrounded && v.option <= OptionFloatTruncated
	}
	return false
}

func (v *Value) WithOption(option Option) {
	if option < OptionFloatRounded || option > OptionFloatTruncated {
		return
	}
	if v.Type() == Float32 || v.Type() == Float64 {
		v.option = option
	}
}

func (v *Value) EqualBytes(b []byte) bool {
	return bytes.Equal(v.data, b)
}

func (v *Value) DisturbingByte() byte {
	return ^v.data[0]
}

func (v *Value) isIntegerFloatWithOption() bool {
	if v.option < OptionFloatRounded || v.option > OptionFloatTruncated {
		return false
	}

	switch v.typ {
	case Float32:
		f := v.ToRaw(Float32).(float32)
		return f == float32(int32(f))
	case Float64:
		f := v.ToRaw(Float64).(float64)
		return f == float64(int64(f))
	default:
		return false
	}
}

func NewInt8(i int8) *Value {
	v := &Value{
		typ:  Int8,
		data: make([]byte, 1),
	}
	v.data[0] = byte(i)
	return v
}

func NewInt16(i int16) *Value {
	v := &Value{
		typ:  Int16,
		data: make([]byte, 2),
	}
	byteOrder.PutUint16(v.data, uint16(i))
	return v
}

func NewInt32(i int32) *Value {
	v := &Value{
		typ:  Int32,
		data: make([]byte, 4),
	}
	byteOrder.PutUint32(v.data, uint32(i))
	return v
}

func NewInt64(i int64) *Value {
	v := &Value{
		typ:  Int64,
		data: make([]byte, 8),
	}
	byteOrder.PutUint64(v.data, uint64(i))
	return v
}

func NewFloat32(i float32, args ...Option) *Value {
	var option Option
	if len(args) > 0 && args[0] <= OptionFloatTruncated {
		option = args[0]
	}

	v := &Value{
		typ:    Float32,
		data:   make([]byte, 4),
		option: option,
	}

	byteOrder.PutUint32(v.data, math.Float32bits(i))
	return v
}

func NewFloat64(i float64, args ...Option) *Value {
	var option Option
	if len(args) > 0 && args[0] <= OptionFloatTruncated {
		option = args[0]
	}
	v := &Value{
		typ:    Float64,
		data:   make([]byte, 8),
		option: option,
	}

	byteOrder.PutUint64(v.data, math.Float64bits(i))
	return v
}

// NewBytes len(b) It should not be greater than 1024(IOV_MAX)
func NewBytes(b []byte) *Value {
	n := len(b)
	v := &Value{
		typ:  Bytes,
		data: make([]byte, n),
	}
	copy(v.data, b)
	return v
}

type ValueComparable interface {
	Size() int
	EqualBytes(b []byte) bool
	DisturbingByte() byte
}
