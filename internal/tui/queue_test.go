package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newViewTestModel(t *testing.T, cfg config.Config, view config.ViewType) Model {
	t.Helper()
	ctx := &context.ProgramContext{
		Config:    &cfg,
		View:      view,
		StartTask: func(task context.Task) tea.Cmd { return nil },
	}
	ctx.Theme = theme.ParseTheme(ctx.Config)
	ctx.Styles = context.InitStyles(ctx.Theme)

	return Model{
		ctx:           ctx,
		keys:          keys.Keys,
		sidebar:       sidebar.NewModel(),
		footer:        footer.NewModel(ctx),
		tabs:          tabs.NewModel(ctx),
		prView:        prview.NewModel(ctx),
		issueSidebar:  issueview.NewModel(ctx),
		branchSidebar: branchsidebar.NewModel(ctx),
	}
}

func parseTestConfig(t *testing.T) config.Config {
	t.Helper()
	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)
	return cfg
}

func TestSwitchSelectedView_Cycle(t *testing.T) {
	tests := []struct {
		name     string
		repoFF   bool
		expected []config.ViewType
	}{
		{
			name:     "without repo view",
			expected: []config.ViewType{config.IssuesView, config.QueueView, config.PRsView},
		},
		{
			name:   "with repo view",
			repoFF: true,
			expected: []config.ViewType{
				config.IssuesView, config.QueueView, config.RepoView, config.PRsView,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.repoFF {
				t.Setenv(config.FF_REPO_VIEW, "1")
			}
			m := newViewTestModel(t, parseTestConfig(t), config.PRsView)

			var visited []config.ViewType
			for range tc.expected {
				m.switchSelectedView()
				visited = append(visited, m.ctx.View)
			}

			require.Equal(t, tc.expected, visited)
		})
	}
}

func queueIssue(number int, assignee string) data.IssueData {
	i := data.IssueData{
		Number:     number,
		Title:      "an issue",
		Body:       "## Parent\n\n#1",
		Repository: data.Repository{NameWithOwner: "owner/repo"},
	}
	if assignee != "" {
		i.Assignees.Nodes = []data.Assignee{{Login: assignee}}
	}
	return i
}

func newQueueTestModel(t *testing.T, startedTasks *[]string) Model {
	t.Helper()
	cfg := parseTestConfig(t)
	cfg.Keybindings.Issues = append(cfg.Keybindings.Issues, config.Keybinding{
		Key:     "e",
		Command: "echo {{.IssueNumber}} {{.RepoName}} {{.RepoPath}}",
	})
	m := newViewTestModel(t, cfg, config.QueueView)
	m.ctx.User = "me"
	m.ctx.RepoPath = "/repo"
	m.ctx.GHRepo = &repository.Repository{Owner: "owner", Name: "repo"}
	runningTasks := map[string]context.Task{}
	m.tasks = runningTasks
	m.ctx.StartTask = func(task context.Task) tea.Cmd {
		*startedTasks = append(*startedTasks, task.StartText)
		runningTasks[task.Id] = task
		return nil
	}

	queueSection := queuesection.NewModel(0, m.ctx, time.Now(), time.Now())
	queueSection.FetchNextPageSectionRows()
	queueSection.Update(queuesection.SectionQueueFetchedMsg{
		Issues: []data.IssueData{queueIssue(5, "me")},
		TaskId: queueSection.LastFetch.TaskId,
	})
	m.queue = []section.Section{&queueSection}
	m.sidebar.IsOpen = true
	m.syncSidebar()
	return m
}

// tea.ExecProcess hides the *exec.Cmd inside an unexported message.
func shellCommandOf(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	require.NotNil(t, cmd)
	execCommand := reflect.ValueOf(cmd()).FieldByName("cmd").Elem().Elem().Field(0)
	require.Equal(t, reflect.TypeFor[*exec.Cmd](), execCommand.Type())
	args := execCommand.Elem().FieldByName("Args")
	return args.Index(args.Len() - 1).String()
}

func TestQueueView_CustomIssueKeybinding(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "e"})

	require.Equal(t, "echo 5 owner/repo /repo", shellCommandOf(t, cmd))
}

func TestQueueView_IssueActions(t *testing.T) {
	tests := []struct {
		key            string
		opensInputBox  bool
		prompt         string
		startsTaskText string
	}{
		{key: "c", opensInputBox: true},
		{key: "L", opensInputBox: true},
		{key: "a", opensInputBox: true},
		{key: "A", opensInputBox: true},
		{key: "x", prompt: "Are you sure you want to close this issue?"},
		{key: "X", prompt: "Are you sure you want to reopen this issue?"},
		{key: "o", startsTaskText: "Opening in browser"},
	}

	for _, tc := range tests {
		t.Run(tc.key, func(t *testing.T) {
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks)

			m.Update(tea.KeyPressMsg{Text: tc.key})

			require.Equal(t, tc.opensInputBox, m.issueSidebar.IsTextInputBoxFocused())
			if tc.prompt != "" {
				require.Contains(t, m.getCurrSection().GetPromptConfirmation(), tc.prompt)
			}
			if tc.startsTaskText != "" {
				require.Contains(t, startedTasks, tc.startsTaskText)
			}
		})
	}
}

// Puts a gh on PATH that succeeds without doing anything, so issue actions
// run their real task and report back as they would after a real gh call.
func stubGh(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// Runs every command reachable from cmd until a task reports it finished.
// Commands run concurrently because some (cursor blinks, ticks) only sleep.
func taskFinishedFrom(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
	t.Helper()
	finished := make(chan constants.TaskFinishedMsg, 1)
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		go func() {
			switch msg := c().(type) {
			case tea.BatchMsg:
				for _, sub := range msg {
					run(sub)
				}
			case constants.TaskFinishedMsg:
				select {
				case finished <- msg:
				default:
				}
			}
		}()
	}
	run(cmd)

	select {
	case msg := <-finished:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no task finished")
		return constants.TaskFinishedMsg{}
	}
}

func currQueueIssue(t *testing.T, m Model) *data.IssueData {
	t.Helper()
	issue, ok := m.getCurrSection().GetCurrRow().(*data.IssueData)
	require.True(t, ok)
	return issue
}

func TestQueueView_IssueActionResultsReachQueueRow(t *testing.T) {
	tests := []struct {
		name   string
		keys   []tea.KeyPressMsg
		assert func(t *testing.T, m Model)
	}{
		{
			name: "comment",
			keys: []tea.KeyPressMsg{{Text: "c"}, {Text: "on it"}, {Code: 'd', Mod: tea.ModCtrl}},
			assert: func(t *testing.T, m Model) {
				comments := currQueueIssue(t, m).Comments.Nodes
				require.Len(t, comments, 1)
				require.Equal(t, "on it", comments[0].Body)
			},
		},
		{
			name: "label",
			keys: []tea.KeyPressMsg{{Text: "L"}, {Text: "blocked"}, {Code: 'd', Mod: tea.ModCtrl}},
			assert: func(t *testing.T, m Model) {
				require.Zero(t, m.getCurrSection().NumRows())
			},
		},
		{
			name: "assign",
			keys: []tea.KeyPressMsg{{Text: "a"}, {Text: "other"}, {Code: 'd', Mod: tea.ModCtrl}},
			assert: func(t *testing.T, m Model) {
				require.Equal(t,
					[]data.Assignee{{Login: "me"}, {Login: "other"}},
					currQueueIssue(t, m).Assignees.Nodes,
				)
			},
		},
		{
			name: "unassign",
			// The input starts filled with the current assignees.
			keys: []tea.KeyPressMsg{{Text: "A"}, {Code: 'd', Mod: tea.ModCtrl}},
			assert: func(t *testing.T, m Model) {
				require.Empty(t, currQueueIssue(t, m).Assignees.Nodes)
			},
		},
		{
			name: "close",
			keys: []tea.KeyPressMsg{{Text: "x"}, {Text: "y"}, {Code: tea.KeyEnter}},
			assert: func(t *testing.T, m Model) {
				require.Equal(t, "CLOSED", currQueueIssue(t, m).State)
			},
		},
		{
			name: "reopen",
			keys: []tea.KeyPressMsg{{Text: "X"}, {Text: "y"}, {Code: tea.KeyEnter}},
			assert: func(t *testing.T, m Model) {
				require.Equal(t, "OPEN", currQueueIssue(t, m).State)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubGh(t)
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks)

			var cmd tea.Cmd
			for _, key := range tc.keys {
				_, cmd = m.Update(key)
			}
			m.Update(taskFinishedFrom(t, cmd))

			tc.assert(t, m)
		})
	}
}

func TestQueueView_RefreshWithoutRepoShowsEmptyTab(t *testing.T) {
	m := newViewTestModel(t, parseTestConfig(t), config.QueueView)
	m.queue = queuesection.InitSections(m.ctx)

	m.Update(tea.KeyPressMsg{Text: "r"})

	queueSection := m.queue[0].(*queuesection.Model)
	require.False(t, queueSection.GetIsLoading())
	require.False(t, queueSection.Table.IsLoading())
	require.Zero(t, queueSection.NumRows())
}
