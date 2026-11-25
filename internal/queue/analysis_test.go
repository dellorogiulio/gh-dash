package queue

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func blockedByIn(number int, title string, labels []string, refs string) data.IssueData {
	return issue(number, title, "## Parent\n\n#1\n\n## Blocked by\n\n"+refs+"\n", labels, nil)
}

type tabRow struct {
	number int
	state  State
}

func tabRows(entries []Entry) []tabRow {
	out := []tabRow{}
	for _, e := range entries {
		out = append(out, tabRow{e.Issue.Number, e.State})
	}
	return out
}

func analyze(issues ...data.IssueData) Analysis {
	return Analyze(issues, "me", nil, "owner", "repo")
}

func TestAnalyzeParksAQualifyingIssueLabelledBlocked(t *testing.T) {
	a := analyze(
		issue(5, "[AFK] parked", parentBody, []string{"blocked"}, nil),
		issue(3, "[AFK] ready", parentBody, nil, nil),
	)

	require.Equal(t, []tabRow{{3, Next}, {5, Parked}}, tabRows(a.Queue))
	require.Equal(t, []tabRow{{5, Parked}}, tabRows(a.NeedsMe))
}

func TestAnalyzeQueueListsCandidatesThenParkedByNumber(t *testing.T) {
	a := analyze(
		issue(9, "[AFK] parked", parentBody, []string{"blocked"}, nil),
		blockedByIn(4, "[AFK] waits on 3", nil, "- #3"),
		issue(7, "[AFK] parked", parentBody, []string{"blocked"}, nil),
		issue(3, "[AFK] ready", parentBody, nil, nil),
		issue(8, "[AFK] parked", parentBody, []string{"blocked"}, nil),
	)

	require.Equal(t,
		[]tabRow{{3, Next}, {4, Blocked}, {7, Parked}, {8, Parked}, {9, Parked}},
		tabRows(a.Queue),
	)
}

func TestAnalyzeLeavesOutIssuesThatAreNotQuiteParked(t *testing.T) {
	a := analyze(
		issue(5, "[AFK] t", parentBody, []string{"blocked", "wontfix"}, nil),
		issue(6, "[AFK] t", parentBody, []string{"blocked"}, []string{"other"}),
		issue(7, "[HITL] t", parentBody, []string{"blocked"}, nil),
		issue(8, "[AFK] t", "no parent", []string{"blocked"}, nil),
	)

	require.Empty(t, a.Queue)
	require.Empty(t, a.NeedsMe)
}

func TestAnalyzeNeedsMeKeepsOnlyHITLIssuesThatUnblockACandidate(t *testing.T) {
	a := analyze(
		issue(2, "[HITL] blocks a candidate", parentBody, nil, nil),
		issue(3, "[AFK] blocks nothing", parentBody, []string{hitlLabel}, nil),
		issue(4, "[HITL] no parent", "", nil, nil),
		issue(5, "[HITL] someone elses", parentBody, nil, []string{"other"}),
		blockedByIn(9, "[AFK] candidate", nil, "- #2\n- #4\n- #5"),
	)

	require.Equal(t, []tabRow{{2, HITL}}, tabRows(a.NeedsMe))
}

func TestAnalyzeMarksHITLAndParkedBlockers(t *testing.T) {
	a := analyze(
		issue(2, "[HITL] decide", parentBody, nil, nil),
		issue(3, "[AFK] t", parentBody, []string{hitlLabel}, nil),
		issue(4, "[AFK] parked", parentBody, []string{"blocked"}, nil),
		issue(5, "[AFK] plain", "", nil, nil),
		blockedByIn(9, "[AFK] candidate", nil, "- #2\n- #3\n- #4\n- #5\n- #6\n- other/lib#1"),
	)

	require.Equal(t, []Blocker{
		{Ref: Ref{Number: 2}, Open: true, HITL: true},
		{Ref: Ref{Number: 3}, Open: true, HITL: true},
		{Ref: Ref{Number: 4}, Open: true, Parked: true},
		{Ref: Ref{Number: 5}, Open: true},
		{Ref: Ref{Number: 6}},
		{Ref: Ref{Owner: "other", Repo: "lib", Number: 1}, Open: true},
	}, a.Queue[0].Blockers)
}

func TestAnalyzeUnblocks(t *testing.T) {
	tests := []struct {
		name     string
		issues   []data.IssueData
		expected map[int]int
	}{
		{
			name: "indirect dependents count",
			issues: []data.IssueData{
				blockedByIn(189, "[AFK] t", nil, "None - can start immediately"),
				blockedByIn(195, "[AFK] t", nil, "- #189"),
				blockedByIn(196, "[AFK] t", nil, "- #195"),
			},
			expected: map[int]int{189: 2, 195: 1, 196: 0},
		},
		{
			name: "a diamond counts the shared dependent once",
			issues: []data.IssueData{
				blockedByIn(1, "[AFK] t", nil, "None - can start immediately"),
				blockedByIn(2, "[AFK] t", nil, "- #1"),
				blockedByIn(3, "[AFK] t", nil, "- #1"),
				blockedByIn(4, "[AFK] t", nil, "- #2\n- #3"),
			},
			expected: map[int]int{1: 3, 2: 1, 3: 1, 4: 0},
		},
		{
			name: "non-candidates are traversed but not counted",
			issues: []data.IssueData{
				blockedByIn(1, "[AFK] t", nil, "None - can start immediately"),
				issue(2, "[AFK] someone elses", "## Blocked by\n\n- #1\n", nil, []string{"other"}),
				blockedByIn(3, "[AFK] t", nil, "- #2"),
			},
			expected: map[int]int{1: 1, 3: 0},
		},
		{
			name: "a 2-cycle ends and excludes the issue itself",
			issues: []data.IssueData{
				blockedByIn(1, "[AFK] t", nil, "- #2"),
				blockedByIn(2, "[AFK] t", nil, "- #1"),
			},
			expected: map[int]int{1: 1, 2: 1},
		},
		{
			name: "a 3-cycle ends and counts each issue once",
			issues: []data.IssueData{
				blockedByIn(1, "[AFK] t", nil, "- #3"),
				blockedByIn(2, "[AFK] t", nil, "- #1"),
				blockedByIn(3, "[AFK] t", nil, "- #2"),
				blockedByIn(4, "[AFK] t", nil, "- #1"),
			},
			expected: map[int]int{1: 3, 2: 3, 3: 3, 4: 0},
		},
		{
			name: "closed blockers link nothing",
			issues: []data.IssueData{
				blockedByIn(2, "[AFK] t", nil, "- #1"),
			},
			expected: map[int]int{2: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := map[int]int{}
			for _, e := range analyze(tt.issues...).Queue {
				got[e.Issue.Number] = e.Unblocks
			}
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestAnalyzeCountsAHITLIssueThroughANonCandidate(t *testing.T) {
	a := analyze(
		issue(1, "[HITL] decide", parentBody, nil, nil),
		issue(2, "[AFK] someone elses", "## Blocked by\n\n- #1\n", nil, []string{"other"}),
		blockedByIn(3, "[AFK] candidate", nil, "- #2"),
	)

	require.Equal(t, []tabRow{{1, HITL}}, tabRows(a.NeedsMe))
	require.Equal(t, 1, a.NeedsMe[0].Unblocks)
}

func TestAnalyzeOrdersNeedsMeByUnblocksThenNumber(t *testing.T) {
	a := analyze(
		issue(1, "[HITL] unblocks one", parentBody, nil, nil),
		issue(2, "[AFK] parked, unblocks none", parentBody, []string{"blocked"}, nil),
		issue(3, "[HITL] unblocks two", parentBody, nil, nil),
		blockedByIn(4, "[AFK] parked, unblocks one", []string{"blocked"}, "- #3"),
		blockedByIn(10, "[AFK] t", nil, "- #1"),
		blockedByIn(11, "[AFK] t", nil, "- #4"),
		blockedByIn(12, "[AFK] t", nil, "- #3"),
	)

	require.Equal(t,
		[]tabRow{{3, HITL}, {1, HITL}, {4, Parked}, {2, Parked}},
		tabRows(a.NeedsMe),
	)
	require.Equal(t, []int{2, 1, 1, 0}, []int{
		a.NeedsMe[0].Unblocks, a.NeedsMe[1].Unblocks, a.NeedsMe[2].Unblocks, a.NeedsMe[3].Unblocks,
	})
}

func TestAnalyzeExposesTheDependencyGraph(t *testing.T) {
	a := analyze(
		blockedByIn(1, "[AFK] t", nil, "None - can start immediately"),
		blockedByIn(2, "[AFK] t", nil, "- #1\n- #7\n- other/lib#1"),
		blockedByIn(3, "[AFK] t", nil, "- #1\n- #2"),
	)

	require.Equal(t, []int{2, 3}, a.Graph.Dependents(1))
	require.Equal(t, []int{1, 2}, a.Graph.Blockers(3))
	require.Empty(t, a.Graph.Blockers(1))
}
