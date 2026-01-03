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

package main

import (
	"strconv"
	"strings"

	"github.com/kayon/memscan"
	"github.com/kayon/memscan/scanner"
)

var version = "0.3.0"

const (
	defRenderResultsThreshold = 10
)

var app *App

type Results struct {
	Count   int
	List    [][2]string
	Round   uint
	Time    string
	CanUndo bool
}

func init() {
	app = &App{
		scan:                   memscan.NewMemscan(),
		renderResultsThreshold: defRenderResultsThreshold,
	}
}

func main() {}

func parseValue(rawValue string, valueType scanner.Type, args ...bool) *scanner.Value {
	// 对于非首次扫描，应该允许零值
	var allowZeroValue bool
	if len(args) > 0 {
		allowZeroValue = args[0]
	}

	rawValue = strings.TrimLeft(rawValue, "0")
	if rawValue == "" {
		rawValue = "0"
	}

	if !allowZeroValue && rawValue == "0" {
		return nil
	}

	var value *scanner.Value
	switch valueType {
	case scanner.Int8:
		if i, err := strconv.ParseUint(rawValue, 10, 8); err == nil {
			value = scanner.NewInt8(int8(i))
		}
	case scanner.Int16:
		if i, err := strconv.ParseUint(rawValue, 10, 16); err == nil {
			value = scanner.NewInt16(int16(i))
		}
	case scanner.Int32:
		if i, err := strconv.ParseUint(rawValue, 10, 32); err == nil {
			value = scanner.NewInt32(int32(i))
		}
	case scanner.Int64:
		if i, err := strconv.ParseUint(rawValue, 10, 64); err == nil {
			value = scanner.NewInt64(int64(i))
		}
	case scanner.Float32:
		if i, err := strconv.ParseFloat(rawValue, 32); err == nil {
			if !allowZeroValue && i == 0 {
				return nil
			}
			value = scanner.NewFloat32(float32(i))
		}
	case scanner.Float64:
		if i, err := strconv.ParseFloat(rawValue, 64); err == nil {
			if !allowZeroValue && i == 0 {
				return nil
			}
			value = scanner.NewFloat64(i)
		}
	default:
		return nil
	}

	return value
}
