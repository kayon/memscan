package main

import (
	"bytes"
	"runtime/pprof"
)

// dumpStacks returns the stack of every goroutine, to see where a call that
// never ends is stuck. It takes no lock: it must work while a scan is running.
func dumpStacks() string {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	return buf.String()
}
