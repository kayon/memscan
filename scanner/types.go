package scanner

// Type All types are memory aligned, except for bytes
type Type uint8

const (
	Bytes Type = iota
	Int8
	Int16
	Int32
	Int64
	Float32
	Float64
)

var typeName = [7]string{"Bytes", "Int8", "Int16", "Int32", "Int64", "Float32", "Float64"}

func (t Type) String() string {
	return typeName[t]
}

func (t Type) ByteSize() int {
	switch t {
	case Int8:
		return 1
	case Int16:
		return 2
	case Int32:
		return 4
	case Int64:
		return 8
	case Float32:
		return 4
	case Float64:
		return 8
	default:
		// Bytes, size depends on input length
		return 0
	}
}

func (t Type) BitSize() int {
	switch t {
	case Int8:
		return 8
	case Int16:
		return 16
	case Int32:
		return 32
	case Int64:
		return 64
	case Float32:
		return 32
	case Float64:
		return 64
	default:
		// Bytes
		return 0
	}
}
