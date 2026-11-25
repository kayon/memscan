package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/kayon/memscan/deck"
	"github.com/kayon/memscan/scanner"
)

// These tests drive the App on their own process, through an anonymous mapping
// filled with known values (see the tests of the memscan package).

const testAppID = 4242

type area struct {
	mem   []byte
	start uint64
}

func newArea(t *testing.T, size int) *area {
	t.Helper()
	mem, err := unix.Mmap(-1, 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Skipf("cannot map memory: %v", err)
	}
	t.Cleanup(func() { _ = unix.Munmap(mem) })
	return &area{mem: mem, start: uint64(uintptr(unsafe.Pointer(&mem[0])))}
}

func (a *area) plant(v *scanner.Value, offsets ...int) (addrs []uint64) {
	for _, off := range offsets {
		copy(a.mem[off:], v.Bytes())
		addrs = append(addrs, a.start+uint64(off))
	}
	return
}

func (a *area) within(addrs []uint64) (kept []uint64) {
	for _, addr := range addrs {
		if addr >= a.start && addr < a.start+uint64(len(a.mem)) {
			kept = append(kept, addr)
		}
	}
	return
}

// slots: one offset per page, in the 16-byte slot slotIndex of the page.
func slots(count, slotIndex int) (offsets []int) {
	for k := 0; k < count; k++ {
		offsets = append(offsets, k*4096+slotIndex*16)
	}
	return
}

func newTestApp(t *testing.T) *App {
	t.Helper()
	proc, err := deck.NewProcess(os.Getpid())
	if err != nil || proc == nil {
		t.Skipf("cannot inspect our own process: %v", err)
	}
	proc.AppID = testAppID
	a := newApp()
	a.game = proc
	a.renderResultsThreshold = MaxResultsThreshold
	for _, s := range a.scans {
		// pausing ourselves would freeze the test
		s.session.SetPauseFuncs(func() error { return nil }, func() error { return nil })
		t.Cleanup(func() { _ = s.session.Close() })
	}
	return a
}

// randomNumber: a decimal text that is an exact float32 too, different on every
// run (copies of a constant pile up in the heap of the test process).
func randomNumber() string {
	return strconv.Itoa(1<<20 + rand.Intn(1<<22))
}

// plantEverywhere writes the number in every type that can hold it, in a slot
// of its own. Each value is followed by a non-zero byte: a 32-bit number
// followed by zeros is also the 64-bit number.
func plantEverywhere(t *testing.T, a *area, number string, pages int) map[scanner.Type][]uint64 {
	t.Helper()
	planted := make(map[scanner.Type][]uint64)
	for i, v := range scanner.ParseCandidates(number, scanner.OptionFloatUnrounded, false) {
		offsets := slots(pages, i)
		planted[v.Type()] = a.plant(v, offsets...)
		for _, off := range offsets {
			a.mem[off+len(v.Bytes())] = 0xFF
		}
	}
	return planted
}

// expectedHits: what a search for the number finds in the area. The bytes of
// a 64-bit number start with the 32-bit number, so every Int64 is also an Int32.
func expectedHits(planted map[scanner.Type][]uint64) map[scanner.Type][]uint64 {
	want := make(map[scanner.Type][]uint64, len(planted))
	for typ, addrs := range planted {
		want[typ] = addrs
	}
	union := append(append([]uint64(nil), planted[scanner.Int32]...), planted[scanner.Int64]...)
	sort.Slice(union, func(i, j int) bool { return union[i] < union[j] })
	want[scanner.Int32] = union
	return want
}

func byName(results *MultiResults) map[string]TypeResults {
	m := make(map[string]TypeResults)
	for _, tr := range results.Types {
		m[tr.Name] = tr
	}
	return m
}

func names(results *MultiResults) (n []string) {
	for _, tr := range results.Types {
		n = append(n, tr.Name)
	}
	return
}

func hasRow(list [][2]string, addr uint64, value string) bool {
	want := [2]string{fmt.Sprintf("%08X", addr), value}
	for _, row := range list {
		if row == want {
			return true
		}
	}
	return false
}

func TestFirstScanMultiSearchesEveryType(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	planted := plantEverywhere(t, mem, number, 10)

	results := a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, 0)
	if results == nil {
		t.Fatal("nil result")
	}
	wantNames := []string{"Int32", "Int64", "Float32", "Float64"} // 20 bits: not an Int8 nor an Int16
	if !reflect.DeepEqual(names(results), wantNames) {
		t.Fatalf("types %v, want %v", names(results), wantNames)
	}
	if results.Round != 1 || results.CanUndo {
		t.Fatalf("round %d, canUndo %v", results.Round, results.CanUndo)
	}

	got := byName(results)
	want := expectedHits(planted)
	for typ := range planted {
		tr := got[typ.String()]
		if tr.Status != "ok" || tr.Type != int(typ) {
			t.Fatalf("%v: %+v", typ, tr)
		}
		if tr.Count < len(want[typ]) {
			t.Fatalf("%v: %d results for %d planted values", typ, tr.Count, len(want[typ]))
		}
		found := mem.within(a.scans[0].session.Scan(typ).Results())
		if !reflect.DeepEqual(found, want[typ]) {
			t.Fatalf("%v: %d hits in the area, want %d", typ, len(found), len(want[typ]))
		}
	}

	// listed, displayed as the user typed it
	if i32 := got["Int32"]; !hasRow(i32.List, planted[scanner.Int32][3], number) {
		t.Fatalf("Int32 list lacks the planted value: %v", i32.List)
	}
}

func TestFirstScanMultiTypesMask(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	plantEverywhere(t, mem, number, 10)

	mask := typesBit(scanner.Int64) | typesBit(scanner.Float32) | typesBit(scanner.Int8) // Int8 cannot hold it
	results := a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, mask)
	if results == nil {
		t.Fatal("nil result")
	}
	if want := []string{"Int64", "Float32"}; !reflect.DeepEqual(names(results), want) {
		t.Fatalf("types %v, want %v", names(results), want)
	}
	if a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, typesBit(scanner.Int8)) != nil {
		t.Fatal("a mask without any type that fits must give nothing")
	}
}

func TestMultiScansNarrowDownThenUndo(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	first, second := randomNumber(), randomNumber()
	plantEverywhere(t, mem, first, 10)

	if a.FirstScanMulti(0, testAppID, first, scanner.OptionFloatUnrounded, false, 0) == nil {
		t.Fatal("first scan failed")
	}

	// the number changes everywhere
	planted2 := plantEverywhere(t, mem, second, 10)
	results := a.NextScanMulti(0, second)
	if results == nil {
		t.Fatal("next scan failed")
	}
	if results.Round != 2 || !results.CanUndo {
		t.Fatalf("round %d, canUndo %v", results.Round, results.CanUndo)
	}
	got := byName(results)
	want := expectedHits(planted2)
	for typ := range planted2 {
		if got[typ.String()].Status != "ok" {
			t.Fatalf("%v: %+v", typ, got[typ.String()])
		}
		if found := mem.within(a.scans[0].session.Scan(typ).Results()); !reflect.DeepEqual(found, want[typ]) {
			t.Fatalf("%v: %d hits kept, want %d", typ, len(found), len(want[typ]))
		}
	}

	// a decimal number fits the floats only, and nothing holds it
	results = a.NextScanMulti(0, "1.5")
	if results == nil {
		t.Fatal("next scan with a decimal failed")
	}
	got = byName(results)
	for name, want := range map[string]string{"Int32": "dropped", "Int64": "dropped", "Float32": "empty", "Float64": "empty"} {
		if got[name].Status != want {
			t.Errorf("%s: status %q, want %q", name, got[name].Status, want)
		}
	}

	// something that is not a number changes nothing
	before := a.scans[0].session.Rounds()
	if r := a.NextScanMulti(0, "abc"); r == nil || r.Round != before {
		t.Fatalf("a text that is not a number must not start a scan: %+v", r)
	}

	// undo brings the dropped and emptied types back
	results = a.UndoScanMulti(0)
	if results == nil {
		t.Fatal("nothing to undo")
	}
	for _, tr := range results.Types {
		if tr.Status != "ok" {
			t.Errorf("%s: status %q after undo", tr.Name, tr.Status)
		}
	}
	if results.Round != 2 {
		t.Errorf("round %d after undo, want 2", results.Round)
	}
}

func TestKeepOnlyMulti(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	plantEverywhere(t, mem, number, 10)

	if a.KeepOnlyMulti(0, typesBit(scanner.Int32)) != nil {
		t.Fatal("KeepOnlyMulti before any scan must give nothing")
	}
	a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, 0) // Int32 Int64 Float32 Float64

	results := a.KeepOnlyMulti(0, typesBit(scanner.Float32) | typesBit(scanner.Int32) | typesBit(scanner.Int8))
	if results == nil {
		t.Fatal("KeepOnlyMulti failed")
	}
	got := byName(results)
	for name, want := range map[string]string{"Int32": "ok", "Float32": "ok", "Int64": "dropped", "Float64": "dropped"} {
		if got[name].Status != want {
			t.Errorf("%s: status %q, want %q", name, got[name].Status, want)
		}
	}
	if got["Int64"].List != nil {
		t.Error("a dropped type must not be listed")
	}
	if results.Round != 1 || !results.CanUndo {
		t.Errorf("round %d, canUndo %v", results.Round, results.CanUndo)
	}

	// none of the requested types has results: nothing changes
	if a.KeepOnlyMulti(0, typesBit(scanner.Int8)|typesBit(scanner.Int64)) != nil {
		t.Error("keeping only dropped or absent types must give nothing")
	}

	results = a.UndoScanMulti(0)
	if results == nil {
		t.Fatal("nothing to undo")
	}
	for _, tr := range results.Types {
		if tr.Status != "ok" {
			t.Errorf("%s: status %q after undo", tr.Name, tr.Status)
		}
	}
}

func TestChangeValuesMultiWritesOneType(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	planted := plantEverywhere(t, mem, number, 10)
	a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, 0)

	readInt32 := func(addr uint64) int32 {
		b := mem.mem[addr-mem.start:]
		return int32(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24)
	}

	idx := positions(t, a, scanner.Int32, planted[scanner.Int32])
	if r := a.ChangeValuesMulti(0, scanner.Int32, "-77", idx); r == nil {
		t.Fatal("change refused")
	}
	for _, addr := range planted[scanner.Int32] {
		if got := readInt32(addr); got != -77 {
			t.Fatalf("%#x holds %d", addr, got)
		}
	}
	// the other types are untouched
	int64Bytes := scanner.ParseAs(number, scanner.Int64, scanner.OptionFloatUnrounded, false).Bytes()
	for _, addr := range planted[scanner.Int64] {
		off := addr - mem.start
		if !reflect.DeepEqual(mem.mem[off:off+8], int64Bytes) {
			t.Fatalf("%#x was written although it is an Int64 result", addr)
		}
	}
	// a value the type cannot hold writes nothing
	if a.ChangeValuesMulti(0, scanner.Int32, "1.5", idx) != nil {
		t.Fatal("1.5 does not fit an Int32")
	}
}

// positions: positions of the addresses in the result list of a type.
func positions(t *testing.T, a *App, typ scanner.Type, addrs []uint64) (indexes []int) {
	t.Helper()
	results := a.scans[0].session.Scan(typ).Results()
	for _, addr := range addrs {
		i := sort.Search(len(results), func(i int) bool { return results[i] >= addr })
		if i < len(results) && results[i] == addr {
			indexes = append(indexes, i)
		}
	}
	if len(indexes) != len(addrs) {
		t.Fatalf("only %d of %d planted %v are in the results", len(indexes), len(addrs), typ)
	}
	return
}

func TestRequestsThatMustGiveNothing(t *testing.T) {
	a := newTestApp(t)
	for _, input := range []string{"", "abc", "0", "0.0", "0x10", "NaN"} {
		if r := a.FirstScanMulti(0, testAppID, input, scanner.OptionFloatUnrounded, false, 0); r != nil {
			t.Errorf("first scan of %q gave %+v", input, r)
		}
	}
	if a.NextScanMulti(0, "1") != nil || a.UndoScanMulti(0) != nil || a.RefreshValuesMulti(0) != nil {
		t.Error("scans before any first scan must give nothing")
	}
	if a.ChangeValuesMulti(0, scanner.Int32, "1", nil) != nil {
		t.Error("change before any first scan must give nothing")
	}
}

// The single-type API of the previous plugin versions keeps working.
func TestSingleTypeAPI(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	planted := plantEverywhere(t, mem, number, 10)

	results := a.FirstScan(testAppID, number, scanner.Int32, scanner.OptionFloatUnrounded, false)
	if results == nil {
		t.Fatal("nil result")
	}
	if results.Round != 1 || results.CanUndo || results.Count < len(planted[scanner.Int32]) {
		t.Fatalf("%+v", results)
	}
	if !hasRow(results.List, planted[scanner.Int32][2], number) {
		t.Fatalf("list lacks the planted value: %v", results.List)
	}
	if got := a.scans[0].session.Infos(); len(got) != 1 || got[0].Type != scanner.Int32 {
		t.Fatalf("a single-type search must search one type: %+v", got)
	}

	// a text the type cannot hold changes nothing
	if r := a.NextScan("1.5"); r == nil || r.Count != results.Count || r.Round != 1 {
		t.Fatalf("NextScan(1.5): %+v", r)
	}

	second := randomNumber()
	plantEverywhere(t, mem, second, 10)
	r := a.NextScan(second)
	if r == nil || r.Round != 2 || !r.CanUndo || r.Count < len(planted[scanner.Int32]) {
		t.Fatalf("NextScan: %+v", r)
	}
	if r = a.UndoScan(); r == nil || r.Round != 1 || r.CanUndo || r.Count != results.Count {
		t.Fatalf("UndoScan: %+v", r)
	}

	idx := positions(t, a, scanner.Int32, planted[scanner.Int32])
	if r = a.ChangeValues("123", idx); r == nil {
		t.Fatal("ChangeValues refused")
	}
	if r = a.RefreshValues(); r == nil || !hasRow(r.List, planted[scanner.Int32][0], "123") {
		t.Fatalf("RefreshValues: %+v", r)
	}
}

// What the plugin reads is part of the interface: field names must not drift.
func TestResultsJSONContract(t *testing.T) {
	a := newTestApp(t)
	mem := newArea(t, 40*4096)
	number := randomNumber()
	plantEverywhere(t, mem, number, 10)
	results := a.FirstScanMulti(0, testAppID, number, scanner.OptionFloatUnrounded, false, 0)

	data, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Types", "Round", "Time", "CanUndo"} {
		if _, ok := generic[key]; !ok {
			t.Errorf("MultiResults has no %q field: %s", key, data)
		}
	}
	first := generic["Types"].([]any)[0].(map[string]any)
	for _, key := range []string{"Type", "Name", "Count", "Status", "List"} {
		if _, ok := first[key]; !ok {
			t.Errorf("TypeResults has no %q field: %s", key, data)
		}
	}

	legacy, _ := json.Marshal(a.single(results))
	var legacyFields map[string]any
	if err := json.Unmarshal(legacy, &legacyFields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Count", "List", "Round", "Time", "CanUndo"} {
		if _, ok := legacyFields[key]; !ok {
			t.Errorf("Results has no %q field: %s", key, legacy)
		}
	}
}
