package main

import (
	"errors"
	"sync"
	"time"

	"github.com/kayon/memscan"
	"github.com/kayon/memscan/deck"
	"github.com/kayon/memscan/scanner"
)

const (
	MinResultsThreshold = 10
	MaxResultsThreshold = 100

	// NumSlots is the number of searches that live side by side (one per thing
	// the user is looking for: ammo, health...). They share the game, nothing else.
	NumSlots = 3
)

// slot is one search: a memscan.Session that scans every type a typed value
// can be searched in at once, and what is needed to display its results.
type slot struct {
	session *memscan.Session
	// the value of each type, as typed last: it gives the display of the results
	values map[scanner.Type]*scanner.Value
	option scanner.Option
	// the type of the single-type API
	legacyType scanner.Type
}

// App is what the plugin talks to: NumSlots independent searches in the same
// game. Every multi-type function takes the number of the slot it works on; a
// number out of range gives nothing. The single-type functions of the previous
// versions (FirstScan, NextScan...) are kept on top of slot 0.
type App struct {
	// mu serializes the exported calls: they can come from different threads
	mu sync.Mutex

	scans [NumSlots]*slot
	game  *deck.Process

	renderResultsThreshold int
}

func newApp() *App {
	app := &App{renderResultsThreshold: defRenderResultsThreshold}
	for i := range app.scans {
		app.scans[i] = &slot{session: memscan.NewSession()}
	}
	return app
}

// slot gives the search number i, nil when there is no such slot.
func (app *App) slot(i int) *slot {
	if i < 0 || i >= NumSlots {
		return nil
	}
	return app.scans[i]
}

// ready tells whether the slot has a search that can be continued.
func (app *App) ready(s *slot) bool {
	return s != nil && app.game != nil && s.values != nil
}

// TypeResults is the state of one type after a scan.
type TypeResults struct {
	Type  int
	Name  string
	Count int
	// ok, parked (too many results to list), truncated (too many to keep),
	// empty (no result left) or dropped (the value does not fit the type)
	Status string
	// the addresses and values, only for a type whose status is ok and whose
	// results are few enough to be listed
	List [][2]string
}

// MultiResults is the state of all the searched types after a scan.
type MultiResults struct {
	Types   []TypeResults
	Round   uint
	Time    string
	CanUndo bool
}

func (app *App) SetRenderResultsThreshold(value int) {
	if value >= MinResultsThreshold && value <= MaxResultsThreshold && value != app.renderResultsThreshold {
		app.renderResultsThreshold = value
	}
}

// Clear forgets every search (the game changed).
func (app *App) Clear() {
	for i := range app.scans {
		app.ResetSlot(i)
	}
}

// ResetSlot forgets the search of one slot, the others stay as they are.
func (app *App) ResetSlot(i int) {
	if s := app.slot(i); s != nil {
		s.session.Reset()
		s.values = nil
	}
}

// ResetScan forgets the search of slot 0 (single-type API).
func (app *App) ResetScan() {
	app.ResetSlot(0)
}

func (app *App) GetGameProcesses(appID int64) []*deck.Process {
	return deck.EnumGameProcesses(appID)
}

func (app *App) SelectGameProcess(appID int64, pid int) *deck.Process {
	if proc, _ := deck.NewProcess(pid); proc != nil {
		proc.AppID = appID
		app.game = proc
	}
	return app.game
}

func (app *App) AutoSelectGameProcess(appID int64) *deck.Process {
	if app.game == nil || (app.game.AppID != appID || !app.game.Alive()) {
		app.game = deck.FindGameWithAppID(appID)
	}
	return app.game
}

// typesBit is the bit of a type in the mask given to FirstScanMulti.
func typesBit(typ scanner.Type) uint {
	return 1 << uint(typ)
}

// FirstScanMulti searches the typed value in every type that can hold it, or
// only in the types of typesMask (one bit per scanner.Type) when it is not 0.
// GameProcess must have been selected before.
func (app *App) FirstScanMulti(slotIndex int, appID int64, value string, option scanner.Option, emulatorMode bool, typesMask uint) *MultiResults {
	s := app.slot(slotIndex)
	if s == nil {
		return nil
	}
	app.AutoSelectGameProcess(appID)
	if app.game == nil {
		return nil
	}

	var candidates []*scanner.Value
	for _, v := range scanner.ParseCandidates(value, option, false) {
		if typesMask == 0 || typesMask&typesBit(v.Type()) != 0 {
			candidates = append(candidates, v)
		}
	}
	if len(candidates) == 0 {
		return nil
	}

	if err := s.session.Open(app.game); err != nil {
		return nil
	}
	dur, err := s.session.FirstScan(candidates, emulatorMode)
	if err != nil {
		return nil
	}

	s.option = option
	s.values = make(map[scanner.Type]*scanner.Value, len(candidates))
	for _, v := range candidates {
		s.values[v.Type()] = v
	}
	return app.render(s, dur)
}

// NextScanMulti keeps, in each type, the results that now hold the value.
// A type the value does not fit is dropped, a type without result left is
// emptied; UndoScanMulti brings them back.
func (app *App) NextScanMulti(slotIndex int, value string) *MultiResults {
	s := app.slot(slotIndex)
	if !app.ready(s) || s.session.Rounds() < 1 {
		return nil
	}
	candidates := scanner.ParseCandidates(value, s.option, true)
	if len(candidates) == 0 {
		// not a number: nothing changes
		return app.render(s, 0)
	}
	return app.nextScan(s, candidates)
}

func (app *App) nextScan(s *slot, candidates []*scanner.Value) *MultiResults {
	dur, err := s.session.NextScan(candidates)
	if err != nil {
		if errors.Is(err, memscan.ErrNoActiveType) {
			return app.render(s, 0)
		}
		return nil
	}
	for _, v := range candidates {
		s.values[v.Type()] = v
	}
	return app.render(s, dur)
}

func (app *App) UndoScanMulti(slotIndex int) *MultiResults {
	s := app.slot(slotIndex)
	if !app.ready(s) {
		return nil
	}
	if s.session.UndoScan() {
		return app.render(s, 0)
	}
	return nil
}

// KeepOnlyMulti drops the types that are not in typesMask (one bit per type
// number) once the user knows which one holds the value. At least one of the
// types must still have results. UndoScanMulti brings the others back.
func (app *App) KeepOnlyMulti(slotIndex int, typesMask uint) *MultiResults {
	s := app.slot(slotIndex)
	if !app.ready(s) {
		return nil
	}
	var types []scanner.Type
	for typ := scanner.Int8; typ <= scanner.Float64; typ++ {
		if typesMask&typesBit(typ) != 0 {
			types = append(types, typ)
		}
	}
	if err := s.session.KeepOnly(types); err != nil {
		return nil
	}
	return app.render(s, 0)
}

// ChangeValuesMulti writes the value over the results of one type designated
// by their position in the list (all of them when indexes is empty).
func (app *App) ChangeValuesMulti(slotIndex int, typ scanner.Type, value string, indexes []int) *MultiResults {
	s := app.slot(slotIndex)
	if !app.ready(s) {
		return nil
	}
	if n := app.count(s, typ); n == 0 || n > app.renderResultsThreshold {
		return nil
	}
	v := scanner.ParseAs(value, typ, s.option, true)
	if v == nil {
		return nil
	}
	if err := s.session.ChangeValues(typ, v, indexes); err != nil {
		return nil
	}
	s.values[typ] = v
	return app.render(s, 0)
}

// RefreshValuesMulti reads the current values of the listed results again.
func (app *App) RefreshValuesMulti(slotIndex int) *MultiResults {
	s := app.slot(slotIndex)
	if !app.ready(s) {
		return nil
	}
	return app.render(s, 0)
}

func (app *App) count(s *slot, typ scanner.Type) int {
	if m := s.session.Scan(typ); m != nil {
		return m.Count()
	}
	return 0
}

func (app *App) render(s *slot, dur time.Duration) *MultiResults {
	results := &MultiResults{
		Round:   s.session.Rounds(),
		CanUndo: s.session.CanUndo(),
	}
	if dur > 0 {
		results.Time = dur.String()
	}
	for _, info := range s.session.Infos() {
		tr := TypeResults{
			Type:   int(info.Type),
			Name:   info.Type.String(),
			Count:  info.Count,
			Status: info.Status.String(),
		}
		if info.Status == memscan.StatusOK && info.Count > 0 && info.Count <= app.renderResultsThreshold {
			if v := s.values[info.Type]; v != nil {
				tr.List, _ = s.session.Render(info.Type, v)
			}
		}
		results.Types = append(results.Types, tr)
	}
	return results
}

// ---------------------------------------------------------------------------
// Single-type API of the previous versions, on top of the multi-type one. It
// is meant for a session that searches one type: FirstScan fixes it. It works
// on slot 0.

type Results struct {
	Count   int
	List    [][2]string
	Round   uint
	Time    string
	CanUndo bool
}

// single extracts the legacy type from a multi-type result.
func (app *App) single(multi *MultiResults) *Results {
	if multi == nil {
		return nil
	}
	results := &Results{Round: multi.Round, Time: multi.Time, CanUndo: multi.CanUndo}
	for _, tr := range multi.Types {
		if scanner.Type(tr.Type) == app.scans[0].legacyType {
			results.Count, results.List = tr.Count, tr.List
		}
	}
	return results
}

// FirstScan searches the value in one type. GameProcess must have been
// selected before.
func (app *App) FirstScan(appID int64, value string, valueType scanner.Type, option scanner.Option, emulatorMode bool) *Results {
	multi := app.FirstScanMulti(0, appID, value, option, emulatorMode, typesBit(valueType))
	if multi == nil {
		return nil
	}
	app.scans[0].legacyType = valueType
	return app.single(multi)
}

func (app *App) NextScan(value string) *Results {
	s := app.scans[0]
	if app.game == nil || s.values[s.legacyType] == nil || s.session.Rounds() < 1 {
		return nil
	}
	v := scanner.ParseAs(value, s.legacyType, s.option, true)
	if v == nil {
		// not a number, or one the type cannot hold: nothing changes
		return app.single(app.render(s, 0))
	}
	return app.single(app.nextScan(s, []*scanner.Value{v}))
}

func (app *App) UndoScan() *Results {
	s := app.scans[0]
	if app.game == nil || s.values[s.legacyType] == nil {
		return nil
	}
	return app.single(app.UndoScanMulti(0))
}

func (app *App) ChangeValues(value string, indexes []int) *Results {
	s := app.scans[0]
	if app.game == nil || s.values[s.legacyType] == nil {
		return nil
	}
	return app.single(app.ChangeValuesMulti(0, s.legacyType, value, indexes))
}

func (app *App) RefreshValues() *Results {
	s := app.scans[0]
	if app.game == nil || s.values[s.legacyType] == nil {
		return nil
	}
	if n := app.count(s, s.legacyType); n == 0 || n > app.renderResultsThreshold {
		return nil
	}
	return app.single(app.render(s, 0))
}
