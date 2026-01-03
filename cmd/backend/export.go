package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"encoding/json"
	"unsafe"

	"github.com/kayon/memscan/scanner"
)

func returnJSON(v interface{}) *C.char {
	data, _ := json.Marshal(v)
	return C.CString(string(data))
}

//export FreeString
func FreeString(p *C.char) {
	C.free(unsafe.Pointer(p))
}

//export SetRenderResultsThreshold
func SetRenderResultsThreshold(value C.int) {
	app.SetRenderResultsThreshold(int(value))
}

//export Version
func Version() *C.char {
	return returnJSON(version)
}

//export Clear
func Clear() {
	app.Clear()
}

//export ResetScan
func ResetScan() {
	app.ResetScan()
}

//export GetGameProcesses
func GetGameProcesses(appID C.int64_t) *C.char {
	processes := app.GetGameProcesses(int64(appID))
	return returnJSON(processes)
}

//export SelectGameProcess
func SelectGameProcess(appID C.int64_t, pid C.int) *C.char {
	process := app.SelectGameProcess(int64(appID), int(pid))
	return returnJSON(process)
}

//export AutoSelectGameProcess
func AutoSelectGameProcess(appID C.int64_t) *C.char {
	process := app.AutoSelectGameProcess(int64(appID))
	return returnJSON(process)
}

//export FirstScan
func FirstScan(appID C.int64_t, value *C.char, valueType C.int, option C.int) *C.char {
	results := app.FirstScan(int64(appID), C.GoString(value), scanner.Type(valueType), scanner.Option(option))
	return returnJSON(results)
}

//export NextScan
func NextScan(value *C.char) *C.char {
	results := app.NextScan(C.GoString(value))
	return returnJSON(results)
}

//export UndoScan
func UndoScan() *C.char {
	results := app.UndoScan()
	return returnJSON(results)
}

//export ChangeValues
func ChangeValues(value *C.char, cIndexes *C.int32_t, length C.int) *C.char {
	idxSlice := unsafe.Slice((*int32)(cIndexes), int(length))
	converted := make([]int, len(idxSlice))
	for i, v := range idxSlice {
		converted[i] = int(v)
	}
	results := app.ChangeValues(C.GoString(value), converted)
	return returnJSON(results)
}

//export RefreshValues
func RefreshValues() *C.char {
	results := app.RefreshValues()
	return returnJSON(results)
}
