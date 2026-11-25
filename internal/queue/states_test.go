package queue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func blockedBy(number int, refs string) data.IssueData {
	return data.IssueData{
		Number: number,
		Body:   "## Parent\n\n#1\n\n## Blocked by\n\n" + refs + "\n",
	}
}

type row struct {
	number int
	state  State
	open   []Ref
}

func rows(entries []Entry) []row {
	out := []row{}
	for _, e := range entries {
		out = append(out, row{e.Issue.Number, e.State, e.OpenBlockers()})
	}
	return out
}

func TestAnalyzeQueueOrder(t *testing.T) {
	tests := []struct {
		name       string
		candidates []data.IssueData
		others     []data.IssueData
		elsewhere  map[Ref]string
		expected   []row
	}{
		{
			name: "only the lowest ready candidate is Next",
			candidates: []data.IssueData{
				blockedBy(7, "None - can start immediately"),
				blockedBy(3, "None - can start immediately"),
				blockedBy(5, "None - can start immediately"),
			},
			expected: []row{{3, Next, nil}, {5, Ready, nil}, {7, Ready, nil}},
		},
		{
			name: "nothing ready means no Next",
			candidates: []data.IssueData{
				blockedBy(4, "- #2"),
				blockedBy(6, "- #2"),
			},
			others:   []data.IssueData{{Number: 2}},
			expected: []row{{4, Blocked, []Ref{{Number: 2}}}, {6, Blocked, []Ref{{Number: 2}}}},
		},
		{
			name:       "a blocker missing from the open issues is closed",
			candidates: []data.IssueData{blockedBy(4, "- #2")},
			expected:   []row{{4, Next, nil}},
		},
		{
			name:       "a blocked candidate lists only its open blockers",
			candidates: []data.IssueData{blockedBy(8, "- #2\n- #3\n- #5")},
			others:     []data.IssueData{{Number: 2}, {Number: 5}},
			expected:   []row{{8, Blocked, []Ref{{Number: 2}, {Number: 5}}}},
		},
		{
			name:       "an unresolved blocker in another repo keeps the candidate Blocked",
			candidates: []data.IssueData{blockedBy(4, "- other/lib#1")},
			elsewhere:  map[Ref]string{{Owner: "other", Repo: "lib", Number: 2}: "CLOSED"},
			expected:   []row{{4, Blocked, []Ref{{Owner: "other", Repo: "lib", Number: 1}}}},
		},
		{
			name:       "an open blocker in another repo keeps the candidate Blocked",
			candidates: []data.IssueData{blockedBy(4, "- #2\n- other/lib#1")},
			elsewhere:  map[Ref]string{{Owner: "other", Repo: "lib", Number: 1}: "OPEN"},
			expected:   []row{{4, Blocked, []Ref{{Owner: "other", Repo: "lib", Number: 1}}}},
		},
		{
			name:       "a closed blocker in another repo frees the candidate",
			candidates: []data.IssueData{blockedBy(4, "- other/lib#1")},
			elsewhere:  map[Ref]string{{Owner: "other", Repo: "lib", Number: 1}: "CLOSED"},
			expected:   []row{{4, Next, nil}},
		},
		{
			name:       "a merged pull request in another repo frees the candidate",
			candidates: []data.IssueData{blockedBy(4, "- other/lib#1")},
			elsewhere:  map[Ref]string{{Owner: "other", Repo: "lib", Number: 1}: "MERGED"},
			expected:   []row{{4, Next, nil}},
		},
		{
			name: "a candidate blocking another one keeps it Blocked",
			candidates: []data.IssueData{
				blockedBy(2, "None - can start immediately"),
				blockedBy(3, "- #2"),
			},
			expected: []row{{2, Next, nil}, {3, Blocked, []Ref{{Number: 2}}}},
		},
		{
			name: "rows are Next, Ready, then Blocked, by number within each group",
			candidates: []data.IssueData{
				blockedBy(1, "- #20"),
				blockedBy(2, "- #10"),
				blockedBy(3, "None - can start immediately"),
				blockedBy(4, "- #99"),
				blockedBy(5, "None - can start immediately"),
			},
			others: []data.IssueData{{Number: 10}, {Number: 20}},
			expected: []row{
				{3, Next, nil},
				{4, Ready, nil},
				{5, Ready, nil},
				{1, Blocked, []Ref{{Number: 20}}},
				{2, Blocked, []Ref{{Number: 10}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			open := append(append([]data.IssueData{}, tt.candidates...), tt.others...)
			entries := Analyze(open, "me", tt.elsewhere, "owner", "repo").Queue
			require.Equal(t, tt.expected, rows(entries))
		})
	}
}
