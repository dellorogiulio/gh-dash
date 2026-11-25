package queue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

const parentBody = "## Parent\n\n#1\n"

func issue(number int, title, body string, labels []string, assignees []string) data.IssueData {
	i := data.IssueData{Number: number, Title: title, Body: body}
	for _, l := range labels {
		i.Labels.Nodes = append(i.Labels.Nodes, data.Label{Name: l})
	}
	for _, a := range assignees {
		i.Assignees.Nodes = append(i.Assignees.Nodes, data.Assignee{Login: a})
	}
	return i
}

func candidateNumbers(issues []data.IssueData) []int {
	out := []int{}
	for _, e := range Analyze(issues, "me", nil, "owner", "repo").Queue {
		if e.State != Parked {
			out = append(out, e.Issue.Number)
		}
	}
	return out
}

func TestCandidatesWiqFixture(t *testing.T) {
	fixture := []data.IssueData{
		issue(
			3,
			"[AFK] Add list",
			"## Parent\n\n#1\n\n## Blocked by\n\n- #2\n- #5\n",
			[]string{"effort:high", "AFK"},
			nil,
		),
		issue(
			2,
			"[AFK] Add schema",
			"## Parent\n\n#1\n\n## Blocked by\n\nNone - can start immediately\n",
			[]string{"effort:low"},
			nil,
		),
		issue(4, "[HITL] Pick provider", parentBody, nil, nil),
		issue(
			6,
			"[AFK] No parent section",
			"## Blocked by\n\nNone - can start immediately\n",
			nil,
			nil,
		),
		issue(7, "[AFK] Labelled hitl", parentBody, []string{"hitl"}, nil),
		issue(8, "[AFK] Someone elses", parentBody, nil, []string{"other"}),
		issue(9, "[AFK] Untagged", parentBody, nil, []string{"me"}),
	}

	require.ElementsMatch(t, []int{2, 3, 9}, candidateNumbers(fixture))
}

func TestCandidatesRules(t *testing.T) {
	tests := []struct {
		name  string
		issue data.IssueData
		kept  bool
	}{
		{"plain issue with a parent", issue(1, "[AFK] t", parentBody, nil, nil), true},
		{"labelled blocked", issue(1, "[AFK] t", parentBody, []string{"blocked"}, nil), false},
		{"labelled wontfix", issue(1, "[AFK] t", parentBody, []string{"wontfix"}, nil), false},
		{
			"labelled needs-info",
			issue(1, "[AFK] t", parentBody, []string{"needs-info"}, nil),
			false,
		},
		{
			"labelled needs-triage",
			issue(1, "[AFK] t", parentBody, []string{"needs-triage"}, nil),
			false,
		},
		{"labelled hitl", issue(1, "[AFK] t", parentBody, []string{"hitl"}, nil), false},
		{
			"skip label among others",
			issue(1, "[AFK] t", parentBody, []string{"AFK", "blocked"}, nil),
			false,
		},
		{
			"label match is case-sensitive",
			issue(1, "[AFK] t", parentBody, []string{"Blocked"}, nil),
			true,
		},
		{"HITL in title", issue(1, "[HITL] Pick provider", parentBody, nil, nil), false},
		{
			"hitl in title is matched case-insensitively",
			issue(1, "needs hitl review", parentBody, nil, nil),
			false,
		},
		{
			"no parent heading",
			issue(1, "[AFK] t", "## Blocked by\n\nNone - can start immediately\n", nil, nil),
			false,
		},
		{
			"### Parent counts as a parent heading",
			issue(1, "[AFK] t", "### Parent\n\n#1\n", nil, nil),
			true,
		},
		{
			"assigned to someone else",
			issue(1, "[AFK] t", parentBody, nil, []string{"other"}),
			false,
		},
		{"assigned to me", issue(1, "[AFK] t", parentBody, nil, []string{"me"}), true},
		{
			"assigned to me among others",
			issue(1, "[AFK] t", parentBody, nil, []string{"other", "me"}),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := candidateNumbers([]data.IssueData{tt.issue})
			require.Equal(t, tt.kept, len(got) == 1)
		})
	}
}
