package queuesection

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestContext(t *testing.T, user string) *context.ProgramContext {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)
	return &context.ProgramContext{
		Config:    &cfg,
		Theme:     thm,
		Styles:    context.InitStyles(thm),
		User:      user,
		GHRepo:    &repository.Repository{Owner: "owner", Name: "repo"},
		StartTask: func(task context.Task) tea.Cmd { return nil },
	}
}

func issue(number int, assignee string, labels ...string) data.IssueData {
	i := data.IssueData{Number: number, Title: "an issue", Body: "## Parent\n\n#1"}
	if assignee != "" {
		i.Assignees.Nodes = []data.Assignee{{Login: assignee}}
	}
	for _, l := range labels {
		i.Labels.Nodes = append(i.Labels.Nodes, data.Label{Name: l})
	}
	return i
}

var openIssues = []data.IssueData{
	issue(9, ""),
	issue(5, "me"),
	issue(7, "someone-else"),
	issue(2, ""),
	issue(4, "", "blocked"),
	{Number: 3, Title: "no parent"},
}

func receiveOpenIssues(m *Model, issues []data.IssueData) {
	m.FetchNextPageSectionRows()
	m.Update(SectionQueueFetchedMsg{Issues: issues, TaskId: m.LastFetch.TaskId})
}

func rowNumbers(m *Model) []int {
	numbers := []int{}
	m.FirstItem()
	for range m.NumRows() {
		numbers = append(numbers, m.GetCurrRow().GetNumber())
		m.NextRow()
	}
	return numbers
}

func TestQueueRows(t *testing.T) {
	tests := []struct {
		name          string
		loginAtFetch  string
		loginLater    string
		expectedFirst []int
		expectedLater []int
	}{
		{
			name:          "login known before the issues",
			loginAtFetch:  "me",
			expectedFirst: []int{2, 5, 9},
		},
		{
			name:          "login arrives after the issues",
			loginAtFetch:  "",
			expectedFirst: []int{2, 9},
			loginLater:    "me",
			expectedLater: []int{2, 5, 9},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext(t, tc.loginAtFetch)
			m := new(NewModel(0, ctx, time.Now(), time.Now()))

			receiveOpenIssues(m, openIssues)
			require.Equal(t, tc.expectedFirst, rowNumbers(m))

			if tc.loginLater != "" {
				ctx.User = tc.loginLater
				m.Update(nil)
				require.Equal(t, tc.expectedLater, rowNumbers(m))
				require.Len(t, m.Table.Rows, len(tc.expectedLater))
			}
		})
	}
}

func TestQueueWithoutGHRepoIsEmpty(t *testing.T) {
	ctx := newTestContext(t, "me")
	ctx.GHRepo = nil
	m := new(NewModel(0, ctx, time.Now(), time.Now()))

	require.Nil(t, m.FetchNextPageSectionRows())
	require.False(t, m.GetIsLoading())
	require.Zero(t, m.NumRows())
}
