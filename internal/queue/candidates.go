// Package queue mirrors the issue picker of wiq (the work issue queue).
//
// The candidate rules follow wiq_candidates in ~/.claude/scripts/wiq-lib.sh,
// which is the source of truth: change them there first, then here. Parked
// and HITL issues have no wiq counterpart.
package queue

import (
	"regexp"
	"slices"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

const (
	parkLabel = "blocked"
	hitlLabel = "hitl"
)

var otherSkipLabels = []string{"wontfix", "needs-info", "needs-triage", hitlLabel}

var (
	hitlTitle     = regexp.MustCompile(`(?i)HITL`)
	parentHeading = regexp.MustCompile(`## Parent`)
)

func candidates(issues []data.IssueData, me string) []data.IssueData {
	out := []data.IssueData{}
	for _, i := range issues {
		if isCandidate(i, me) {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, byNumber)
	return out
}

func byNumber(a, b data.IssueData) int {
	return a.Number - b.Number
}

func isCandidate(i data.IssueData, me string) bool {
	return !hasAnyLabel(i, []string{parkLabel}) && qualifies(i, me)
}

func isParked(i data.IssueData, me string) bool {
	return hasAnyLabel(i, []string{parkLabel}) && qualifies(i, me)
}

func qualifies(i data.IssueData, me string) bool {
	return !hasAnyLabel(i, otherSkipLabels) && !hasHITLTitle(i) && hasParent(i) && isFreeOrMine(i, me)
}

func isHITL(i data.IssueData) bool {
	return hasHITLTitle(i) || hasAnyLabel(i, []string{hitlLabel})
}

func hasAnyLabel(i data.IssueData, names []string) bool {
	return slices.ContainsFunc(i.Labels.Nodes, func(l data.Label) bool {
		return slices.Contains(names, l.Name)
	})
}

func hasHITLTitle(i data.IssueData) bool {
	return hitlTitle.MatchString(i.Title)
}

func hasParent(i data.IssueData) bool {
	return parentHeading.MatchString(i.Body)
}

func isFreeOrMine(i data.IssueData, me string) bool {
	return len(i.Assignees.Nodes) == 0 ||
		slices.ContainsFunc(i.Assignees.Nodes, func(a data.Assignee) bool { return a.Login == me })
}
