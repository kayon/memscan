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
	"fmt"
	"os"

	"github.com/kayon/memscan"
	"github.com/kayon/memscan/deck"
	"github.com/kayon/memscan/scanner"

	"github.com/spf13/pflag"
)

var (
	// Basic flags (Global switches)
	showAllProcesses       bool
	showGameProcesses      bool
	customTest             bool
	findGameWithInstanceID int
	findGameWithAppID      int64
)

func init() {
	// Process list switches
	pflag.BoolVarP(&showAllProcesses, "all-processes", "a", false, "display all running processes")
	pflag.BoolVarP(&showGameProcesses, "game-processes", "g", false, "display only game-related processes")
	pflag.BoolVarP(&customTest, "test", "t", false, "Test")
	pflag.IntVar(&findGameWithInstanceID, "instance", 0, "find game process with instance ID")
	pflag.Int64Var(&findGameWithAppID, "appid", -1, "find game process with app ID")

	// Execute the parsing
	pflag.Parse()
}

func main() {
	switch {
	case customTest:
		customTestFunc()
	case showAllProcesses:
		processes, err := deck.EnumDeckProcesses()
		checkError(err)
		displayProcesses(processes)
	case showGameProcesses:
		processes := deck.EnumGameProcesses()
		displayProcesses(processes)
	case findGameWithInstanceID > 0:
		process := deck.FindGameWithInstanceID(findGameWithInstanceID)
		if process != nil {
			displayProcesses([]*deck.Process{process})
		}
	case findGameWithAppID > -1:
		process := deck.FindGameWithAppID(findGameWithAppID)
		if process != nil {
			displayProcesses([]*deck.Process{process})
		}
	default:
		runConsole()
	}
}

func checkError(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func customTestFunc() {
	processes := deck.EnumGameProcesses()
	if len(processes) == 0 {
		return
	}
	process := processes[0]
	displayProcesses([]*deck.Process{process})

	scan := memscan.NewMemscan()
	err := scan.Open(process)
	checkError(err)
	defer scan.Close()

	value := scanner.NewInt32(1)

	process.Pause()
	defer process.Resume()

	dur := scan.FirstScan(value, true)
	fmt.Printf("FirstScan: %d, %s\n", scan.Count(), dur)

	dur = scan.NextScanForceDense(value)
	fmt.Printf("NextScanForceDense: %d, %s\n", scan.Count(), dur)

	scan.UndoScan()
	fmt.Printf("Undo: %d\n", scan.Count())

	dur = scan.NextScanForceSparse(value)
	fmt.Printf("NextScanForceSparse: %d, %s\n", scan.Count(), dur)
}
