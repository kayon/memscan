package main

import (
	"fmt"
	"math"
	"memscan/deck"
	"memscan/scanner"
	"os"
	"os/signal"
	"time"

	"memscan"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
)

const (
	ConsoleStepSelectGame = iota
	ConsoleStepSelectType
	ConsoleStepEnterScanValue
	ConsoleStepEnterChangeValue
	ConsoleStepFirstScan
	ConsoleStepNext
	ConsoleStepNextScan
	ConsoleExit
)

var (
	colorLabel     = color.New(color.FgYellow)
	colorHighlight = color.New(color.FgGreen)
)

type Console struct {
	step        uint8
	selectIndex int
	value       *scanner.Value
	scanCount   int
	mscan       *memscan.Memscan
	lastScan    time.Duration
	quit        chan os.Signal
}

func (console *Console) Close() error {
	return console.mscan.Close()
}

func (console *Console) Run() {
Loop:
	for {
		switch console.step {
		case ConsoleStepSelectGame:
			console.selectGame()
		case ConsoleStepSelectType:
			console.selectValueType()
		case ConsoleStepEnterScanValue,
			ConsoleStepEnterChangeValue:
			console.enterScanValue()
		case ConsoleStepFirstScan:
			console.firstScan()
		case ConsoleStepNext:
			console.next()
		case ConsoleStepNextScan:
			console.nextScan()
		default:
			break Loop
		}
	}
	close(console.quit)
}

func (console *Console) label() string {
	var help string
	var label string
	switch console.step {
	case ConsoleStepSelectGame:
		help = "Press [J] [K] to navigate"
		label = colorLabel.Sprintf("<SELECT GAME>")
	case ConsoleStepSelectType:
		help = "Press [J] [K] to navigate"
		label = colorLabel.Sprintf("<VALUE TYPE>")
	case ConsoleStepEnterScanValue:
		if console.mscan.Rounds() == 0 {
			help = "Enter first scan value"
			label = colorLabel.Sprintf("<SCAN VALUE>")
		} else {
			help = "Enter next scan value"
			label = colorLabel.Sprintf("<NEXT SCAN>")
		}
	case ConsoleStepEnterChangeValue:
		results := console.mscan.Results()
		if console.selectIndex > -1 {
			help = fmt.Sprintf("%08X", results[console.selectIndex])
		} else {
			help = "All"
		}
		label = colorLabel.Sprintf("<CHANGE VALUE>")
	case ConsoleStepNext:
		help = "Press [J] [K] to navigate"
		label = colorLabel.Sprintf("%s (%s)", console.mscan, console.lastScan)
	}

	return fmt.Sprintf("%s [%s]", label, help)
}

func (console *Console) selectGame() {
	processes := deck.EnumGameProcesses()
	if len(processes) == 0 {
		color.Red("ERROR: No game is running.")
		os.Exit(1)
	}
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> [{{ .PID }}] {{ .Comm | green }}",
		Inactive: "  [{{ .PID }}] {{ .Comm }}",
		Selected: "Game > [{{ .PID }}] {{ .Comm | green }}",
		Details: `
──────────────────── Process ────────────────────
{{ "Name:" | faint }}	{{ .Comm }}
{{ "PID:" | faint }}	{{ .PID | cyan }}
{{ "PPID:" | faint }}	{{ .PPID }}
{{ "PGRP:" | faint }}	{{ .PGRP }}
{{ "State:" | faint }}	{{ .State | yellow }}
{{ "Command:" | faint }}	{{ .Command }}`,
	}

	prompt := promptui.Select{
		Label:     console.label(),
		Items:     processes,
		Templates: templates,
		Size:      6,
	}
	prompt.HideHelp = true

	i, _, err := prompt.Run()
	console.checkError(err)

	err = console.mscan.Open(processes[i])
	console.checkError(err)
	console.step = ConsoleStepSelectType
}

func (console *Console) selectValueType() {
	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ . | red }}",
		Inactive: "  {{ . }}",
		Selected: "Scan Type > {{ . | red }}",
	}

	items := []scanner.Type{
		scanner.Bytes,
		scanner.Int8,
		scanner.Int16,
		scanner.Int32,
		scanner.Int64,
		scanner.Float32,
		scanner.Float64,
	}

	prompt := promptui.Select{
		Label:     console.label(),
		Items:     items,
		Templates: templates,
		Size:      7,
	}
	prompt.HideHelp = true

	i, _, err := prompt.RunCursorAt(3, 0)
	console.checkError(err)

	console.value.SetType(items[i])
	console.step = ConsoleStepEnterScanValue
}

func (console *Console) enterScanValue() {
	templates := &promptui.PromptTemplates{
		Prompt:  "{{ . }} ",
		Valid:   "{{ . | green }} ",
		Invalid: "{{ . | red }} ",
		Success: "{{ . }} ",
	}

	validate := func(input string) error {
		err := console.value.FromString(input)
		if err != nil {
			return &inputError{typ: console.value.Type()}
		}
		return nil
	}

	prompt := promptui.Prompt{
		Label:     console.label(),
		Templates: templates,
		Validate:  validate,
	}

	value, err := prompt.Run()
	console.checkError(err)

	isNextScan := console.mscan.Rounds() > 0
	isChangeValue := console.step == ConsoleStepEnterChangeValue

	label := "Scan Value"
	var retIndexes = make([]int, 0, 1)
	if isChangeValue {
		label = "Change All"
		if console.selectIndex > -1 {
			retIndexes = append(retIndexes, console.selectIndex)
		}
	}

	fmt.Printf("\u001B[1A\u001B[2K\r%s > %s (%s)\n", label, color.RedString(value), console.value.String())

	if isChangeValue {
		console.mscan.ChangeResultsValues(retIndexes, console.value)
		console.step = ConsoleStepNext
	} else if isNextScan {
		console.step = ConsoleStepNextScan
	} else {
		console.step = ConsoleStepFirstScan
	}
}

func (console *Console) firstScan() {
	console.lastScan = console.mscan.FirstScan(console.value)
	console.step = ConsoleStepNext
}

func (console *Console) next() {
	console.selectIndex = -1

	templates := &promptui.SelectTemplates{
		Label:    "{{ . }}",
		Active:   "> {{ . | red }}",
		Inactive: "  {{ . }}",
	}

	var items []string
	var opts int
	var count = console.mscan.Count()

	if console.mscan.Count() <= 10 {
		display := console.mscan.RenderResults(console.value)
		opts = len(display)
		items = make([]string, 0, opts+2)
		for i, v := range display {
			items = append(items, fmt.Sprintf("%2d. [%s] %s", i, v[0], v[1]))
		}
	}
	items = append(items, "Next Scan")
	if count == 0 {
		items[count] = "New Scan"
	} else {
		items = append(items, "New Scan")
		if count > 1 && opts > 0 {
			items = append(items, "Change All")
		}
	}

	prompt := promptui.Select{
		Label:     console.label(),
		Items:     items,
		Templates: templates,
		Size:      12,
	}
	prompt.HideHelp = true

	i, _, err := prompt.RunCursorAt(0, 0)
	console.checkError(err)

	fmt.Print("\u001B[1A\u001B[2K")

	switch i {
	// Next Scan | New Scan
	case opts:
		if count == 0 {
			console.mscan.Reset()
		}
		console.step = ConsoleStepEnterScanValue
	// New Scan
	case opts + 1:
		console.mscan.Reset()
		console.step = ConsoleStepSelectType
	// Change All
	case opts + 2:
		console.step = ConsoleStepEnterChangeValue
	// Change Value
	default:
		console.selectIndex = i
		console.step = ConsoleStepEnterChangeValue
	}
}

func (console *Console) nextScan() {
	console.lastScan = console.mscan.NextScan(console.value)
	console.step = ConsoleStepNext
}

func (console *Console) checkError(err error) {
	if err != nil {
		if err != promptui.ErrInterrupt && err != promptui.ErrEOF {
			color.Red("ERROR: %v", err)
		}
		os.Exit(1)
	}
}

func runConsole() {
	console := &Console{
		mscan: memscan.NewMemscan(),
		value: &scanner.Value{},
		quit:  make(chan os.Signal, 1),
	}

	signal.Notify(console.quit, os.Interrupt)
	go console.Run()
	<-console.quit
	_ = console.Close()
}

type inputError struct {
	typ scanner.Type
}

func (e *inputError) Error() string {
	var help string
	switch e.typ {
	case scanner.Int8:
		help = fmt.Sprintf("Int8: 8bit (%d to %d) or (0 to %d)", math.MinInt8, math.MaxInt8, math.MaxUint8)
	case scanner.Int16:
		help = fmt.Sprintf("Int16: 16bit (%d to %d) or (0 to %d)", math.MinInt16, math.MaxInt16, math.MaxUint16)
	case scanner.Int32:
		help = fmt.Sprintf("Int32: 32bit (%d to %d) or (0 to %d)", math.MinInt32, math.MaxInt32, math.MaxUint32)
	case scanner.Int64:
		help = fmt.Sprintf("Int64: 64bit (%d to %d) or (0 to %d)", math.MinInt64, math.MaxInt64, uint64(math.MaxUint64))
	case scanner.Float32:
		help = fmt.Sprintf("Float32: 32bit (max: %g)", math.MaxFloat32)
	case scanner.Float64:
		help = fmt.Sprintf("Float64: 64bit (max: %g)", math.MaxFloat64)
	case scanner.Bytes:
		help = `Bytes: should be a hex string, e.g. "FF 01"`
	}
	return fmt.Sprintf("invalid input. %s", help)
}
