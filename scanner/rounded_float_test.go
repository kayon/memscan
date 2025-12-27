package scanner

import (
	"testing"
)

func TestRoundedFloat_RangeValidation(t *testing.T) {
	t.Run("Float32_Ranges", func(t *testing.T) {
		tests := []struct {
			input   float32
			option  Option
			wantMin float32
			wantMax float32
		}{
			// Rounded: Round(100.2) -> 100 -> [99.5, 100.5)
			{100.2, OptionFloatRounded, 99.5, 100.5},
			// Rounded: Round(100.8) -> 101 -> [100.5, 101.5)
			{100.8, OptionFloatRounded, 100.5, 101.5},
			// Truncated: Trunc(100.9) -> 100 -> [100.0, 101.0)
			{100.9, OptionFloatTruncated, 100.0, 101.0},
			// Extreme: 100.0 -> (99.00001, 101.0]
			{100.0, OptionFloatExtreme, 100.0 - 1.0 + float32Epsilon, 101.0},
		}

		for _, tt := range tests {
			val := NewFloat32(tt.input, tt.option)
			rf := newRoundedFloat32(*val)

			if rf.min != tt.wantMin || rf.max != tt.wantMax {
				t.Errorf("F32 %s(%f): got [%f, %f], want [%f, %f]",
					tt.option, tt.input, rf.min, rf.max, tt.wantMin, tt.wantMax)
			}
		}
	})

	t.Run("Float64_Ranges", func(t *testing.T) {
		tests := []struct {
			input   float64
			option  Option
			wantMin float64
			wantMax float64
		}{
			// Rounded: Round(100.2) -> 100 -> [99.5, 100.5)
			{100.2, OptionFloatRounded, 99.5, 100.5},
			// Rounded: Round(100.8) -> 101 -> [100.5, 101.5)
			{100.8, OptionFloatRounded, 100.5, 101.5},
			// Truncated: Trunc(100.9) -> 100 -> [100.0, 101.0)
			{100.9, OptionFloatTruncated, 100.0, 101.0},
			// Extreme: 100.0 -> (99.00001, 101.0]
			{100.0, OptionFloatExtreme, 100.0 - 1.0 + float64Epsilon, 101.0},
		}

		for _, tt := range tests {
			val := NewFloat64(tt.input, tt.option)
			rf := newRoundedFloat64(*val)

			if rf.min != tt.wantMin || rf.max != tt.wantMax {
				t.Errorf("F64 %s(%f): got [%f, %f], want [%f, %f]",
					tt.option, tt.input, rf.min, rf.max, tt.wantMin, tt.wantMax)
			}
		}
	})
}
