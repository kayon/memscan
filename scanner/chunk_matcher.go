// Copyright (C) 2026 naparuba
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

import "bytes"

// ChunkMatcher finds the occurrences of one Value inside an in-memory chunk.
//
// Unlike Scanner it does not read anything: the caller reads a region once and
// hands the same chunk to the matcher of every searched type (single pass,
// multi-pattern).
//
// Contract:
//   - offset is the position of chunk[0] inside the scanned region and must be
//     a multiple of 8 (the largest value size), so that the alignment of a
//     value inside the chunk is its alignment inside the region;
//   - every chunk except the last one must have a length that is a multiple of
//     8. Under these two conditions an aligned value can never straddle two
//     chunks, so no data has to be carried over between calls;
//   - the collector receives offset+position of each match, in ascending order.
//     Returning false stops the scan of the chunk (honoured by exact matchers
//     only: the vectorized float scanners always run to the end of the chunk,
//     like Scanner does).
//
// The matchers are stateless and safe for concurrent use.
type ChunkMatcher interface {
	Type() Type
	ScanChunk(chunk []byte, offset int, collector CollectorFunc)
}

// NewChunkMatcher returns the matcher equivalent to what Scanner does for the
// same Value, or nil for Bytes: arbitrary-length, unaligned patterns can
// straddle chunks and still need Scanner.
func NewChunkMatcher(value Value) ChunkMatcher {
	switch {
	case value.typ == Bytes:
		return nil
	case value.typ == Float32 && value.HasOption():
		rf := newRoundedFloat32(value)
		return &float32RangeMatcher{min: rf.min, max: rf.max}
	case value.typ == Float64 && value.HasOption():
		rf := newRoundedFloat64(value)
		return &float64RangeMatcher{min: rf.min, max: rf.max}
	default:
		return &exactMatcher{
			typ:     value.typ,
			pattern: append([]byte(nil), value.data...),
		}
	}
}

// exactMatcher looks for the raw bytes of a value, on the value's alignment.
type exactMatcher struct {
	typ     Type
	pattern []byte
}

func (m *exactMatcher) Type() Type { return m.typ }

func (m *exactMatcher) ScanChunk(chunk []byte, offset int, collector CollectorFunc) {
	size := len(m.pattern)
	if size == 0 {
		return
	}
	for i := 0; i+size <= len(chunk); {
		n := bytes.Index(chunk[i:], m.pattern)
		if n < 0 {
			return
		}
		pos := i + n
		if misalign := (offset + pos) % size; misalign != 0 {
			// not on a value boundary: resume at the next boundary
			i = pos + (size - misalign)
			continue
		}
		if !collector(offset + pos) {
			return
		}
		i = pos + size
	}
}

// float32RangeMatcher matches float32 values in [min, max) (rounded, extreme
// and truncated options).
type float32RangeMatcher struct {
	min, max float32
}

func (m *float32RangeMatcher) Type() Type { return Float32 }

func (m *float32RangeMatcher) ScanChunk(chunk []byte, offset int, collector CollectorFunc) {
	if n := len(chunk) / 4 * 4; n >= 4 {
		scanFloat32Vectorized(chunk[:n], m.min, m.max, offset, collector)
	}
}

// float64RangeMatcher matches float64 values in [min, max).
type float64RangeMatcher struct {
	min, max float64
}

func (m *float64RangeMatcher) Type() Type { return Float64 }

func (m *float64RangeMatcher) ScanChunk(chunk []byte, offset int, collector CollectorFunc) {
	if n := len(chunk) / 8 * 8; n >= 8 {
		scanFloat64Vectorized(chunk[:n], m.min, m.max, offset, collector)
	}
}
