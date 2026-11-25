// Copyright (C) 2026 naparuba
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

package memscan

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kayon/memscan/scanner"
)

var (
	ErrNoActiveType  = errors.New("memscan: no type has results to refine")
	ErrTypeNotActive = errors.New("memscan: the type has no usable results")

	ErrTooManyResults = errors.New("memscan: too many results to read at once")
)

// undoState lets UndoScan put back what the last NextScan (or KeepOnly) changed.
type undoState struct {
	// status of every type before the change
	status map[scanner.Type]TypeStatus
	// types on which Memscan.NextScan ran (and can be undone). The other
	// types were left untouched, whatever status they got.
	ran []scanner.Type
	// true when a NextScan started this state: it counted as a round
	scan bool
}

func (s *Session) snapshotStatus() map[scanner.Type]TypeStatus {
	snapshot := make(map[scanner.Type]TypeStatus, len(s.status))
	for typ, status := range s.status {
		snapshot[typ] = status
	}
	return snapshot
}

// Rounds is the number of scans done so far (the first scan is round 1).
func (s *Session) Rounds() uint { return s.round }

// CanUndo tells whether UndoScan can revert the last NextScan.
func (s *Session) CanUndo() bool { return s.undo != nil }

// activeTypes lists the types that can be refined, in Type order.
func (s *Session) activeTypes() (types []scanner.Type) {
	for _, typ := range sessionScanTypesInOrder {
		if m, ok := s.scans[typ]; ok && s.status[typ].Active() && m.Count() > 0 {
			types = append(types, typ)
		}
	}
	return
}

// activeScan returns the scan of a type that still has results.
func (s *Session) activeScan(typ scanner.Type) (*Memscan, error) {
	m, ok := s.scans[typ]
	if !ok || !s.status[typ].Active() {
		return nil, fmt.Errorf("%w: %v", ErrTypeNotActive, typ)
	}
	return m, nil
}

// NextScan keeps, for each type, the results that now hold the given value.
//
// values holds the new value expressed in each type it can be expressed in.
// An active type without a value (the value does not fit it, e.g. 300 in an
// Int8) is dropped; a type left without any result is marked empty. Both can
// be brought back with UndoScan. Values of types that are not active are
// ignored.
//
// The previous scan can be undone once: starting a new scan makes the types
// dropped by the previous one final, and frees their memory.
func (s *Session) NextScan(values []*scanner.Value) (time.Duration, error) {
	if s.proc == nil {
		return 0, ErrSessionNotOpen
	}
	byType := make(map[scanner.Type]*scanner.Value, len(values))
	for _, v := range values {
		if v == nil {
			continue
		}
		if _, dup := byType[v.Type()]; dup {
			return 0, fmt.Errorf("%w: %v", ErrDuplicateType, v.Type())
		}
		byType[v.Type()] = v
	}
	active := s.activeTypes()
	if len(active) == 0 {
		return 0, ErrNoActiveType
	}
	if !s.proc.Alive() {
		return 0, ErrProcessGone
	}
	if s.ctx.Err() != nil {
		s.renewContext()
	}

	// the previous undo window closes: free what only it was keeping
	s.forgetInactive()

	undo := &undoState{status: s.snapshotStatus(), scan: true}

	st := time.Now()
	var wg sync.WaitGroup
	for _, typ := range active {
		v := byType[typ]
		if v == nil {
			s.status[typ] = StatusDropped
			continue
		}
		undo.ran = append(undo.ran, typ)
		wg.Add(1)
		go func(m *Memscan, v *scanner.Value) {
			defer wg.Done()
			m.NextScan(v)
		}(s.scans[typ], v)
	}
	wg.Wait()

	s.round++
	s.undo = undo
	if s.ctx.Err() != nil {
		// canceled: the lists are partial, go back to the state before the scan
		s.UndoScan()
		return 0, ErrScanCanceled
	}

	for _, typ := range undo.ran {
		switch n := s.scans[typ].Count(); {
		case n == 0:
			s.status[typ] = StatusEmpty
		case n > s.limits.Soft:
			s.status[typ] = StatusParked
		default:
			s.status[typ] = StatusOK
		}
	}

	return time.Since(st), nil
}

// UndoScan reverts the last NextScan, including the types it dropped. It
// reports false when there is nothing to undo.
func (s *Session) UndoScan() bool {
	if s.undo == nil {
		return false
	}
	for _, typ := range s.undo.ran {
		if m := s.scans[typ]; m != nil {
			m.UndoScan()
		}
	}
	s.status = s.undo.status
	if s.undo.scan && s.round > 1 {
		s.round--
	}
	s.undo = nil
	return true
}

// KeepOnly drops every active type that is not in types: it is what you do
// once you know which type holds the value. The dropped types are given up for
// good when the next scan starts, until then UndoScan brings them back.
//
// Undo goes back to before the last NextScan when there was one, otherwise
// only the types come back. Nothing happens, and nothing can be undone, when
// no active type would be dropped. At least one of types must be active.
func (s *Session) KeepOnly(types []scanner.Type) error {
	if s.proc == nil {
		return ErrSessionNotOpen
	}
	keep := make(map[scanner.Type]bool, len(types))
	for _, typ := range types {
		keep[typ] = true
	}
	var kept, dropped []scanner.Type
	for _, typ := range s.activeTypes() {
		if keep[typ] {
			kept = append(kept, typ)
		} else {
			dropped = append(dropped, typ)
		}
	}
	if len(kept) == 0 {
		return ErrTypeNotActive
	}
	if len(dropped) == 0 {
		return nil
	}

	// a state left by a NextScan stays: undoing it brings everything back
	if s.undo == nil {
		s.undo = &undoState{status: s.snapshotStatus()}
	}
	for _, typ := range dropped {
		s.status[typ] = StatusDropped
	}
	return nil
}

// forgetInactive frees the results of the types a previous scan dropped or
// emptied: once a new scan starts they can no longer be brought back.
func (s *Session) forgetInactive() {
	for typ, m := range s.scans {
		switch s.status[typ] {
		case StatusDropped, StatusEmpty:
			m.results.Clear()
			m.prevResults.Clear()
		}
	}
}

// ChangeValues writes value over the results of typ designated by indexes
// (positions in the result list); no index means all of them.
func (s *Session) ChangeValues(typ scanner.Type, value *scanner.Value, indexes []int) error {
	m, err := s.activeScan(typ)
	if err != nil {
		return err
	}
	m.ChangeResultsValues(indexes, value)
	return nil
}

// WriteAddresses writes value at the given addresses, which must come from the
// results of typ (frozen values are tracked by address).
func (s *Session) WriteAddresses(typ scanner.Type, value *scanner.Value, addresses []uint64) error {
	m, err := s.activeScan(typ)
	if err != nil {
		return err
	}
	m.ChangeValues(addresses, value)
	return nil
}

// Render reads the current value at each result of typ and formats it like
// value does. It is meant for short lists: more than IOV_MAX results are
// refused (the caller decides the display threshold, which is far below).
func (s *Session) Render(typ scanner.Type, value *scanner.Value) ([][2]string, error) {
	m, err := s.activeScan(typ)
	if err != nil {
		return nil, err
	}
	if m.Count() > IOV_MAX {
		return nil, fmt.Errorf("%w: %d results of %v", ErrTooManyResults, m.Count(), typ)
	}
	return m.RenderResults(value), nil
}
