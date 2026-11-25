// Package queue mirrors the issue picker of wiq (the work issue queue).
//
// The rules follow wiq_candidates in ~/.claude/scripts/wiq-lib.sh, which is
// the source of truth: change them there first, then here.
package queue

import (
	"regexp"
	"slices"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

var skipLabels = []string{"blocked", "wontfix", "needs-info", "needs-triage", "hitl"}

var (
	hitlTitle     = regexp.MustCompile(`(?i)HITL`)
	parentHeading = regexp.MustCompile(`## Parent`)
)

func Candidates(issues []data.IssueData, me string) []data.IssueData {
	out := []data.IssueData{}
	for _, i := range issues {
		if !hasAnyLabel(i, skipLabels) && !hasHITLTitle(i) && hasParent(i) && isFreeOrMine(i, me) {
			out = append(out, i)
		}
	}
	slices.SortFunc(out, func(a, b data.IssueData) int { return a.Number - b.Number })
	return out
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
