package main

import (
	"fmt"
	"memscan/deck"
	"os"

	"github.com/spf13/pflag"
)

var (
	// Basic flags (Global switches)
	showAllProcesses       bool
	showGameProcesses      bool
	findGameWithInstanceID int
	findGameWithAppID      int64
)

func init() {
	// Process list switches
	pflag.BoolVarP(&showAllProcesses, "all-processes", "a", false, "display all running processes")
	pflag.BoolVarP(&showGameProcesses, "game-processes", "g", false, "display only game-related processes")
	pflag.IntVar(&findGameWithInstanceID, "instance", 0, "find game process with instance ID")
	pflag.Int64Var(&findGameWithAppID, "appid", 0, "find game process with app ID")

	// Execute the parsing
	pflag.Parse()
}

func main() {
	switch {
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
	case findGameWithAppID > 0:
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
