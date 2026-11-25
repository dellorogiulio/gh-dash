package queue

import (
	"cmp"
	"slices"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

type Analysis struct {
	Queue   []Entry
	NeedsMe []Entry
	Graph   Graph
}

type analyzer struct {
	me                      string
	open                    map[int]data.IssueData
	otherRepoStates         map[Ref]string
	launchOwner, launchRepo string
	graph                   Graph
}

func Analyze(
	openIssues []data.IssueData,
	me string,
	otherRepoStates map[Ref]string,
	launchOwner, launchRepo string,
) Analysis {
	openIssues = slices.SortedFunc(slices.Values(openIssues), byNumber)
	a := analyzer{
		me:              me,
		open:            map[int]data.IssueData{},
		otherRepoStates: otherRepoStates,
		launchOwner:     launchOwner,
		launchRepo:      launchRepo,
		graph:           newGraph(openIssues, launchOwner, launchRepo),
	}
	for _, i := range openIssues {
		a.open[i.Number] = i
	}

	var parked, hitl []Entry
	for _, i := range openIssues {
		switch {
		case isParked(i, me):
			parked = append(parked, a.entry(i, Parked))
		case isHITL(i) && hasParent(i) && isFreeOrMine(i, me):
			if entry := a.entry(i, HITL); entry.Unblocks > 0 {
				hitl = append(hitl, entry)
			}
		}
	}

	needsMe := slices.Concat(parked, hitl)
	slices.SortFunc(needsMe, func(x, y Entry) int {
		return cmp.Or(cmp.Compare(y.Unblocks, x.Unblocks), cmp.Compare(x.Issue.Number, y.Issue.Number))
	})
	return Analysis{
		Queue:   slices.Concat(a.classify(candidates(openIssues, me)), parked),
		NeedsMe: needsMe,
		Graph:   a.graph,
	}
}

func (a analyzer) entry(i data.IssueData, state State) Entry {
	entry := Entry{Issue: i, State: state, Unblocks: a.unblocks(i.Number)}
	for _, ref := range Blockers(i.Body, a.launchOwner, a.launchRepo) {
		entry.Blockers = append(entry.Blockers, a.blocker(ref))
	}
	return entry
}

func (a analyzer) unblocks(number int) int {
	count := 0
	for _, d := range a.graph.downstream(number) {
		if isCandidate(a.open[d], a.me) {
			count++
		}
	}
	return count
}
