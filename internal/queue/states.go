package queue

import (
	"cmp"
	"slices"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

type State int

const (
	Next State = iota
	Ready
	Blocked
	Parked
	HITL
)

type Blocker struct {
	Ref
	Open   bool
	HITL   bool
	Parked bool
}

type Entry struct {
	Issue    data.IssueData
	State    State
	Blockers []Blocker
	Unblocks int
}

func (e Entry) OpenBlockers() []Ref {
	var refs []Ref
	for _, b := range e.Blockers {
		if b.Open {
			refs = append(refs, b.Ref)
		}
	}
	return refs
}

func (a analyzer) classify(candidates []data.IssueData) []Entry {
	entries := []Entry{}
	for _, c := range candidates {
		entry := a.entry(c, Ready)
		if len(entry.OpenBlockers()) > 0 {
			entry.State = Blocked
		}
		entries = append(entries, entry)
	}

	slices.SortFunc(entries, func(a, b Entry) int {
		return cmp.Or(cmp.Compare(a.State, b.State), cmp.Compare(a.Issue.Number, b.Issue.Number))
	})
	if len(entries) > 0 && entries[0].State == Ready {
		entries[0].State = Next
	}
	return entries
}

// A blocker in the launch repo is open iff it is among the open issues; one in
// another repo is open unless otherRepoStates gives it a state other than OPEN.
func (a analyzer) blocker(ref Ref) Blocker {
	if ref.Owner != "" {
		state, resolved := a.otherRepoStates[ref]
		return Blocker{Ref: ref, Open: !resolved || state == "OPEN"}
	}
	i, open := a.open[ref.Number]
	return Blocker{Ref: ref, Open: open, HITL: isHITL(i), Parked: isParked(i, a.me)}
}
