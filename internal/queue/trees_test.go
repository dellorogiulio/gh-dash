package queue

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

var stateNames = map[State]string{
	Next: "Next", Ready: "Ready", Blocked: "Blocked", Parked: "Parked", HITL: "HITL",
}

func outline(node Node) []string {
	lines := []string{}
	var walk func(n Node, depth int)
	walk = func(n Node, depth int) {
		for _, child := range n.Children {
			status := map[NodeStatus]string{Outside: "open", Closed: "closed"}[child.Status]
			if child.Status == Listed {
				status = stateNames[child.State]
			}
			line := fmt.Sprintf("%s%s %s", strings.Repeat("  ", depth), child.Ref, status)
			if child.Cycle {
				line += " ↻"
			}
			lines = append(lines, line)
			walk(child, depth+1)
		}
	}
	walk(node, 0)
	return lines
}

func TestDependencyTrees(t *testing.T) {
	chain := []data.IssueData{
		issue(2, "[AFK] first", parentBody, nil, nil),
		blockedByIn(3, "[AFK] second", nil, "- #2"),
		blockedByIn(4, "[AFK] third", nil, "- #3"),
		blockedByIn(5, "[AFK] fourth", nil, "- #4"),
	}

	tests := []struct {
		name      string
		issues    []data.IssueData
		elsewhere map[Ref]string
		tree      func(Analysis, int) Node
		root      int
		expected  []string
	}{
		{
			name:     "upstream follows blockers of blockers",
			issues:   chain,
			tree:     Analysis.UpstreamTree,
			root:     5,
			expected: []string{"#4 Blocked", "  #3 Blocked", "    #2 Next"},
		},
		{
			name:     "downstream follows dependents of dependents",
			issues:   chain,
			tree:     Analysis.DownstreamTree,
			root:     2,
			expected: []string{"#3 Blocked", "  #4 Blocked", "    #5 Blocked"},
		},
		{
			name: "an open issue in neither tab is drawn and expanded",
			issues: []data.IssueData{
				issue(2, "[AFK] first", parentBody, nil, nil),
				issue(3, "[AFK] someone elses", "## Blocked by\n\n- #2\n", nil, []string{"other"}),
				blockedByIn(4, "[AFK] mine", nil, "- #3"),
			},
			tree:     Analysis.UpstreamTree,
			root:     4,
			expected: []string{"#3 open", "  #2 Next"},
		},
		{
			name: "a closed blocker of the launch repo is a leaf",
			issues: []data.IssueData{
				blockedByIn(5, "[AFK] waits", nil, "- #9"),
			},
			tree:     Analysis.UpstreamTree,
			root:     5,
			expected: []string{"#9 closed"},
		},
		{
			name: "blockers in other repos are leaves, open unless looked up otherwise",
			issues: []data.IssueData{
				blockedByIn(5, "[AFK] waits", nil, "- other/lib#1\n- other/lib#2\n- other/lib#3"),
			},
			elsewhere: map[Ref]string{
				{Owner: "other", Repo: "lib", Number: 1}: "CLOSED",
				{Owner: "other", Repo: "lib", Number: 2}: "OPEN",
			},
			tree:     Analysis.UpstreamTree,
			root:     5,
			expected: []string{"other/lib#1 closed", "other/lib#2 open", "other/lib#3 open"},
		},
		{
			name: "a node reached through two branches is drawn in both",
			issues: []data.IssueData{
				issue(2, "[AFK] shared", parentBody, nil, nil),
				blockedByIn(3, "[AFK] left", nil, "- #2"),
				blockedByIn(4, "[AFK] right", nil, "- #2"),
				blockedByIn(5, "[AFK] joins", nil, "- #3\n- #4"),
			},
			tree:     Analysis.UpstreamTree,
			root:     5,
			expected: []string{"#3 Blocked", "  #2 Next", "#4 Blocked", "  #2 Next"},
		},
		{
			name: "upstream stops at an ancestor of a 2-cycle",
			issues: []data.IssueData{
				blockedByIn(3, "[AFK] a", nil, "- #4"),
				blockedByIn(4, "[AFK] b", nil, "- #3"),
			},
			tree:     Analysis.UpstreamTree,
			root:     3,
			expected: []string{"#4 Blocked", "  #3 Blocked ↻"},
		},
		{
			name: "downstream stops at an ancestor of a 2-cycle",
			issues: []data.IssueData{
				blockedByIn(3, "[AFK] a", nil, "- #4"),
				blockedByIn(4, "[AFK] b", nil, "- #3"),
			},
			tree:     Analysis.DownstreamTree,
			root:     3,
			expected: []string{"#4 Blocked", "  #3 Blocked ↻"},
		},
		{
			name:     "upstream stops at a self-cycle",
			issues:   []data.IssueData{blockedByIn(3, "[AFK] a", nil, "- #3")},
			tree:     Analysis.UpstreamTree,
			root:     3,
			expected: []string{"#3 Blocked ↻"},
		},
		{
			name:     "downstream stops at a self-cycle",
			issues:   []data.IssueData{blockedByIn(3, "[AFK] a", nil, "- #3")},
			tree:     Analysis.DownstreamTree,
			root:     3,
			expected: []string{"#3 Blocked ↻"},
		},
		{
			name:     "an issue without dependencies has empty trees",
			issues:   chain,
			tree:     Analysis.UpstreamTree,
			root:     2,
			expected: []string{},
		},
		{
			name:     "an issue nothing waits on has an empty downstream tree",
			issues:   chain,
			tree:     Analysis.DownstreamTree,
			root:     5,
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Analyze(tt.issues, "me", tt.elsewhere, "owner", "repo")
			require.Equal(t, tt.expected, outline(tt.tree(a, tt.root)))
		})
	}
}
