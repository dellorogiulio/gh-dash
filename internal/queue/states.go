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
)

type Blocker struct {
	Ref
	Open bool
}

type Entry struct {
	Issue    data.IssueData
	State    State
	Blockers []Blocker
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

// Classify orders the candidates Next, Ready, then Blocked. A blocker in the
// launch repo is open iff it is among the open issues; one in another repo is
// taken as open.
func Classify(
	candidates, openIssues []data.IssueData,
	launchOwner, launchRepo string,
) []Entry {
	openNumbers := map[int]bool{}
	for _, i := range openIssues {
		openNumbers[i.Number] = true
	}

	entries := []Entry{}
	for _, c := range candidates {
		entry := Entry{Issue: c, State: Ready}
		for _, ref := range Blockers(c.Body, launchOwner, launchRepo) {
			open := ref.Owner != "" || openNumbers[ref.Number]
			entry.Blockers = append(entry.Blockers, Blocker{Ref: ref, Open: open})
			if open {
				entry.State = Blocked
			}
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
