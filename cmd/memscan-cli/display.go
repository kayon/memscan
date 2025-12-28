package main

import (
	"fmt"

	"gihub.com/kayon/memscan/deck"
	"github.com/fatih/color"
)

func displayProcesses(processes []*deck.Process) {
	var (
		paddingPID  int
		paddingPPID int
		paddingPGRP int
		paddingComm int
	)

	for _, p := range processes {
		pid := fmt.Sprint(p.PID)
		ppid := fmt.Sprint(p.PPID)
		pgrp := fmt.Sprint(p.PGRP)
		if n := len(pid); n > paddingPID {
			paddingPID = n
		}
		if n := len(ppid); n > paddingPPID {
			paddingPPID = n
		}
		if n := len(pgrp); n > paddingPGRP {
			paddingPGRP = n
		}
		if n := len(p.Comm); n > paddingComm {
			paddingComm = n
		}
	}

	for _, p := range processes {
		_, _ = fmt.Print(fmt.Sprintf("%s %*d %*d %s %s %q",
			color.CyanString("%*d", paddingPID, p.PID),
			paddingPPID, p.PPID,
			paddingPGRP, p.PGRP,
			color.YellowString(p.State.String()),
			color.GreenString("%-*s", paddingComm, p.Comm),
			p.Command,
		))
		fmt.Println()
	}
}
