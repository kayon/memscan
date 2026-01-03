// Copyright (C) 2025 kayon <kayon.hu@gmail.com>
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package scanner

import "math"

const (
	float32Epsilon = 1e-5
	float64Epsilon = 1e-9
)

var (
	_ ValueComparable = &RoundedFloat32{}
	_ ValueComparable = &RoundedFloat64{}
)

type RoundedFloat32 struct {
	min float32
	max float32
	raw []byte
}

func (rf32 RoundedFloat32) Size() int {
	return 4
}

func (rf32 RoundedFloat32) EqualBytes(b []byte) bool {
	v := math.Float32frombits(byteOrder.Uint32(b))
	return v >= rf32.min && v < rf32.max
}

func (rf32 RoundedFloat32) DisturbingByte() byte {
	return ^rf32.raw[0]
}

type RoundedFloat64 struct {
	min float64
	max float64
	raw []byte
}

func (rf64 RoundedFloat64) Size() int {
	return 8
}

func (rf64 RoundedFloat64) EqualBytes(b []byte) bool {
	v := math.Float64frombits(byteOrder.Uint64(b))
	return v >= rf64.min && v < rf64.max
}

func (rf64 RoundedFloat64) DisturbingByte() byte {
	return ^rf64.raw[0]
}

func newRoundedFloat32(value Value) *RoundedFloat32 {
	i := value.ToRaw(Float32).(float32)
	var rf = &RoundedFloat32{
		raw: make([]byte, 4),
	}
	copy(rf.raw, value.data)

	switch value.option {
	case OptionFloatRounded:
		r := float32(math.Round(float64(i)))
		rf.min, rf.max = r-0.5, r+0.5
	case OptionFloatExtreme:
		rf.min, rf.max = i-1.0+float32Epsilon, i+1.0
	case OptionFloatTruncated:
		r := float32(math.Trunc(float64(i)))
		rf.min, rf.max = r, r+1.0
	}
	return rf
}

func newRoundedFloat64(value Value) *RoundedFloat64 {
	i := value.ToRaw(Float64).(float64)
	var rf = &RoundedFloat64{
		raw: make([]byte, 8),
	}
	copy(rf.raw, value.data)

	switch value.option {
	case OptionFloatRounded:
		r := math.Round(i)
		rf.min, rf.max = r-0.5, r+0.5
	case OptionFloatExtreme:
		rf.min, rf.max = i-1.0+float64Epsilon, i+1.0
	case OptionFloatTruncated:
		r := math.Trunc(i)
		rf.min, rf.max = r, r+1.0
	}
	return rf
}
