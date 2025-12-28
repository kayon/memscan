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

	"github.com/kayon/memscan"
	"github.com/kayon/memscan/scanner"
)

var version = "0.2.1"

const (
	defRenderResultsThreshold = 10
)

var app *App

type Results struct {
	Count int
	List  [][2]string
	Round uint
	Time  string
}

func init() {
	app = &App{
		scan:                   memscan.NewMemscan(),
		renderResultsThreshold: defRenderResultsThreshold,
	}
}

func main() {}

func parseValue(rawValue string, valueType scanner.Type) *scanner.Value {
	var value *scanner.Value
	switch valueType {
	case scanner.Int8:
		if i, err := strconv.ParseInt(rawValue, 10, 8); err == nil && i != 0 {
			value = scanner.NewInt8(int8(i))
		}
	case scanner.Int16:
		if i, err := strconv.ParseInt(rawValue, 10, 16); err == nil && i != 0 {
			value = scanner.NewInt16(int16(i))
		}
	case scanner.Int32:
		if i, err := strconv.ParseInt(rawValue, 10, 32); err == nil && i != 0 {
			value = scanner.NewInt32(int32(i))
		}
	case scanner.Int64:
		if i, err := strconv.ParseInt(rawValue, 10, 64); err == nil && i != 0 {
			value = scanner.NewInt64(i)
		}
	case scanner.Float32:
		if i, err := strconv.ParseFloat(rawValue, 32); err == nil && i != 0 {
			value = scanner.NewFloat32(float32(i))
		}
	case scanner.Float64:
		if i, err := strconv.ParseFloat(rawValue, 64); err == nil && i != 0 {
			value = scanner.NewFloat64(i)
		}
	default:
		return nil
	}

	return value
}
