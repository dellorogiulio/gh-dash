package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/cli/go-gh/v2/pkg/repository"
	zone "github.com/lrstanley/bubblezone/v2"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
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

func queueIssue(number int, assignee string, labels ...string) data.IssueData {
	i := data.IssueData{
		Number:     number,
		Title:      "an issue",
		Body:       "## Parent\n\n#1",
		Repository: data.Repository{NameWithOwner: "owner/repo"},
	}
	if assignee != "" {
		i.Assignees.Nodes = []data.Assignee{{Login: assignee}}
	}
	for _, l := range labels {
		i.Labels.Nodes = append(i.Labels.Nodes, data.Label{Name: l})
	}
	return i
}

func newQueueTestModel(t *testing.T, startedTasks *[]string, issues ...data.IssueData) Model {
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

	m.queue = queuesection.InitSections(m.ctx)
	queueTab := m.queue[0].(*queuesection.Model)
	queueTab.FetchNextPageSectionRows()
	queueTab.Update(queuesection.SectionQueueFetchedMsg{
		Issues: issues,
		TaskId: queueTab.LastFetch.TaskId,
	})
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
	m := newQueueTestModel(t, &startedTasks, queueIssue(5, "me"))

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
			m := newQueueTestModel(t, &startedTasks, queueIssue(5, "me"))

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
				require.Equal(t,
					[]data.Label{{Name: "blocked"}},
					currQueueIssue(t, m).Labels.Nodes,
				)
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
			m := newQueueTestModel(t, &startedTasks, queueIssue(5, "me"))

			var cmd tea.Cmd
			for _, key := range tc.keys {
				_, cmd = m.Update(key)
			}
			m.Update(taskFinishedFrom(t, cmd))

			tc.assert(t, m)
		})
	}
}

func TestQueueView_RefreshWithoutRepoShowsEmptyTabs(t *testing.T) {
	for _, currTab := range []int{0, 1} {
		m := newViewTestModel(t, parseTestConfig(t), config.QueueView)
		m.queue = queuesection.InitSections(m.ctx)
		m.setCurrSectionId(currTab)

		m.Update(tea.KeyPressMsg{Text: "r"})

		require.Len(t, m.queue, 2)
		for _, s := range m.queue {
			tab := s.(*queuesection.Model)
			require.False(t, tab.GetIsLoading())
			require.False(t, tab.Table.IsLoading())
			require.Zero(t, tab.NumRows())
		}
	}
}

func TestQueueView_IssueActionResultsReachNeedsMeRow(t *testing.T) {
	stubGh(t)
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, queueIssue(5, "me", "blocked"))
	m.setCurrSectionId(1)
	m.syncSidebar()

	var cmd tea.Cmd
	for _, key := range []tea.KeyPressMsg{
		{Text: "c"}, {Text: "on it"}, {Code: 'd', Mod: tea.ModCtrl},
	} {
		_, cmd = m.Update(key)
	}
	finished := taskFinishedFrom(t, cmd)
	m.Update(finished)

	require.Equal(t, 1, finished.SectionId)
	comments := currQueueIssue(t, m).Comments.Nodes
	require.Len(t, comments, 1)
	require.Equal(t, "on it", comments[0].Body)
}

func TestQueueView_UnparkingAnIssueUpdatesBothTabs(t *testing.T) {
	stubGh(t)
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, queueIssue(5, "me", "blocked"), queueIssue(6, "me"))
	m.setCurrSectionId(1)
	m.syncSidebar()

	var cmd tea.Cmd
	for _, key := range []tea.KeyPressMsg{
		{Text: "L"}, {Code: 'u', Mod: tea.ModCtrl}, {Text: "bug"}, {Code: 'd', Mod: tea.ModCtrl},
	} {
		_, cmd = m.Update(key)
	}
	m.Update(taskFinishedFrom(t, cmd))

	require.Zero(t, m.queue[1].NumRows())
	queueTab := m.queue[0].(*queuesection.Model)
	states := []string{}
	for _, row := range queueTab.Table.Rows {
		states = append(states, ansi.Strip(row[0]))
	}
	require.Equal(t, []string{"🟢 Next", "🟢 Ready"}, states)
}

func dependentIssues() []data.IssueData {
	blocker := queueIssue(2, "")
	blocker.Title = "[AFK] Lay the rails"
	waiting := queueIssue(3, "")
	waiting.Title = "[AFK] Run the train"
	waiting.Body += "\n\n## Blocked by\n\n- #2\n"
	return []data.IssueData{blocker, waiting}
}

func TestQueueView_PreviewShowsTheDependencyTrees(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, dependentIssues()...)

	view := ansi.Strip(m.issueSidebar.View())

	require.Contains(t, view, "Unblocks")
	require.Contains(t, view, "└─ 🔴 #3 [AFK] Run the train")
}

func TestIssuesView_PreviewHasNoDependencyTrees(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, dependentIssues()...)
	m.ctx.View = config.IssuesView
	m.issues = issuessection.InitSections(m.ctx)
	m.issues[0].(*issuessection.Model).Issues = dependentIssues()

	m.syncSidebar()

	view := ansi.Strip(m.issueSidebar.View())
	require.Contains(t, view, "#2 · owner/repo")
	require.NotContains(t, view, "Unblocks")
	require.NotContains(t, view, "└─")
}

func chainedIssues() []data.IssueData {
	issues := dependentIssues()
	last := queueIssue(4, "")
	last.Title = "[AFK] Reach the station"
	last.Body += "\n\n## Blocked by\n\n- #3\n"
	return append(issues, last)
}

func highlightedLine(t *testing.T, m Model) string {
	t.Helper()
	var highlighted []string
	for line := range strings.SplitSeq(ansi.Strip(m.issueSidebar.View()), "\n") {
		if strings.Contains(line, "▶") {
			highlighted = append(highlighted, strings.TrimSpace(line))
		}
	}
	require.LessOrEqual(t, len(highlighted), 1)
	if len(highlighted) == 0 {
		return ""
	}
	return highlighted[0]
}

func TestQueueView_TabHighlightsTheTreeNodesInTurn(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Text: "j"})
	require.Equal(t, 3, currQueueIssue(t, m).Number)
	require.Empty(t, highlightedLine(t, m))

	var highlighted []string
	for range 3 {
		m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		highlighted = append(highlighted, highlightedLine(t, m))
	}

	require.Equal(t, []string{
		"└─ ▶ 🟢 #2 [AFK] Lay the rails",
		"└─ ▶ 🔴 #4 [AFK] Reach the station",
		"└─ ▶ 🟢 #2 [AFK] Lay the rails",
	}, highlighted)
}

func TestQueueView_HighlightResetsWhenTheRowChanges(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.NotEmpty(t, highlightedLine(t, m))

	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Text: "k"})

	require.Empty(t, highlightedLine(t, m))
}

func TestQueueView_HighlightResetsWhenTheRowChangesWithThePreviewClosed(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, "└─ ▶ 🟢 #2 [AFK] Lay the rails", highlightedLine(t, m))

	m.Update(tea.KeyPressMsg{Text: "p"})
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Text: "p"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, 4, currQueueIssue(t, m).Number)
	require.Empty(t, highlightedLine(t, m))
	view := ansi.Strip(m.issueSidebar.View())
	require.Contains(t, view, "#4 · owner/repo")
	require.Contains(t, view, "Blocked by")
	require.NotContains(t, view, "Unblocks")
}

func refetchQueue(m *Model, issues []data.IssueData) {
	queueTab := m.queue[0].(*queuesection.Model)
	queueTab.FetchNextPageSectionRows()
	m.Update(constants.TaskFinishedMsg{
		SectionId:   0,
		SectionType: queuesection.SectionType,
		TaskId:      queueTab.LastFetch.TaskId,
		Msg: queuesection.SectionQueueFetchedMsg{
			Issues: issues,
			TaskId: queueTab.LastFetch.TaskId,
		},
	})
}

func TestQueueView_HighlightResetsWhenTheAnalysisChanges(t *testing.T) {
	another := queueIssue(5, "")
	another.Title = "[AFK] Build the bridge"
	another.Body += "\n\n## Blocked by\n\n- #2\n"
	changed := append(chainedIssues(), another)

	tests := []struct {
		name        string
		refetched   []data.IssueData
		highlighted string
	}{
		{
			name:        "same issues",
			refetched:   chainedIssues(),
			highlighted: "└─ ▶ 🔴 #3 [AFK] Run the train",
		},
		{name: "changed issues", refetched: changed, highlighted: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			require.Equal(t, 2, currQueueIssue(t, m).Number)

			refetchQueue(&m, tc.refetched)

			require.Equal(t, 2, currQueueIssue(t, m).Number)
			require.Equal(t, tc.highlighted, highlightedLine(t, m))
		})
	}
}

func TestQueueView_HighlightResetsWhenTheAnalysisChangesWithThePreviewClosed(t *testing.T) {
	another := queueIssue(5, "")
	another.Title = "[AFK] Build the bridge"
	another.Body += "\n\n## Blocked by\n\n- #2\n"
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, "└─ ▶ 🔴 #3 [AFK] Run the train", highlightedLine(t, m))

	m.Update(tea.KeyPressMsg{Text: "p"})
	refetchQueue(&m, append(chainedIssues(), another))
	m.Update(tea.KeyPressMsg{Text: "p"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, 2, currQueueIssue(t, m).Number)
	require.Empty(t, highlightedLine(t, m))
	require.Contains(t, ansi.Strip(m.issueSidebar.View()), "#5 [AFK] Build the bridge")
}

func requireNoHighlightAndNoJump(t *testing.T, m Model, startedTasks *[]string) {
	t.Helper()
	require.Empty(t, highlightedLine(t, m))
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	require.Equal(t, config.QueueView, m.ctx.View)
	require.Equal(t, 0, m.currSectionId)
	require.Equal(t, 2, currQueueIssue(t, m).Number)
	require.NotContains(t, *startedTasks, "Opening in browser")
}

func TestQueueView_HighlightStaysResetWhenAChangeIsUndoneWithThePreviewClosed(t *testing.T) {
	another := queueIssue(5, "")
	another.Title = "[AFK] Build the bridge"
	another.Body += "\n\n## Blocked by\n\n- #2\n"

	tests := []struct {
		name string
		undo func(m *Model)
	}{
		{name: "row moved away and back", undo: func(m *Model) {
			m.Update(tea.KeyPressMsg{Text: "j"})
			m.Update(tea.KeyPressMsg{Text: "k"})
		}},
		{name: "analysis changed and changed back", undo: func(m *Model) {
			refetchQueue(m, append(chainedIssues(), another))
			refetchQueue(m, chainedIssues())
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			require.Equal(t, "└─ ▶ 🔴 #3 [AFK] Run the train", highlightedLine(t, m))

			m.Update(tea.KeyPressMsg{Text: "p"})
			tc.undo(&m)
			m.Update(tea.KeyPressMsg{Text: "p"})

			require.Equal(t, 2, currQueueIssue(t, m).Number)
			requireNoHighlightAndNoJump(t, m, &startedTasks)
		})
	}
}

func TestQueueView_HighlightResetsAfterARoundTripThroughTheOtherViews(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	require.Equal(t, "└─ ▶ 🔴 #3 [AFK] Run the train", highlightedLine(t, m))

	var visited []config.ViewType
	for range 3 {
		m.Update(tea.KeyPressMsg{Text: "s"})
		visited = append(visited, m.ctx.View)
	}

	require.Equal(t, []config.ViewType{config.PRsView, config.IssuesView, config.QueueView}, visited)
	require.Equal(t, 2, currQueueIssue(t, m).Number)
	requireNoHighlightAndNoJump(t, m, &startedTasks)
}

func TestQueueView_HighlightSurvivesAResize(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	m.Update(tea.WindowSizeMsg{Width: 300, Height: 60})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	require.Equal(t, "└─ ▶ 🔴 #4 [AFK] Reach the station", highlightedLine(t, m))
}

func TestQueueView_JumpingToACandidateBlockerSelectsItsRow(t *testing.T) {
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Text: "j"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, 0, m.currSectionId)
	require.Equal(t, 2, currQueueIssue(t, m).Number)
	require.Contains(t, ansi.Strip(m.issueSidebar.View()), "#2 · owner/repo")
}

func TestQueueView_JumpingToAHITLBlockerSwitchesToNeedsMe(t *testing.T) {
	decision := queueIssue(2, "")
	decision.Title = "[HITL] Pick the gauge"
	waiting := queueIssue(3, "")
	waiting.Title = "[AFK] Lay the rails"
	waiting.Body += "\n\n## Blocked by\n\n- #2\n"
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, decision, waiting)
	require.Equal(t, 3, currQueueIssue(t, m).Number)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	require.Equal(t, 1, m.currSectionId)
	require.Equal(t, 2, currQueueIssue(t, m).Number)
	require.Contains(t, ansi.Strip(m.issueSidebar.View()), "#2 · owner/repo")
}

// Points GH_BROWSER at a script that records the URL it is asked to open.
func stubBrowser(t *testing.T) (openedUrl func() string) {
	t.Helper()
	dir := t.TempDir()
	opened := filepath.Join(dir, "opened")
	script := filepath.Join(dir, "browser")
	require.NoError(
		t,
		os.WriteFile(script, []byte("#!/bin/sh\nprintf %s \"$1\" > "+opened+"\n"), 0o755),
	)
	t.Setenv("GH_BROWSER", script)
	return func() string {
		url, err := os.ReadFile(opened)
		require.NoError(t, err)
		return string(url)
	}
}

func TestQueueView_JumpingToABlockerOutsideTheTabsOpensItInTheBrowser(t *testing.T) {
	tests := []struct {
		name    string
		blocker string
		url     string
	}{
		{name: "closed", blocker: "#9", url: "https://github.com/owner/repo/issues/9"},
		{name: "other repo", blocker: "other/lib#4", url: "https://github.com/other/lib/issues/4"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			openedUrl := stubBrowser(t)
			waiting := queueIssue(3, "")
			waiting.Body += "\n\n## Blocked by\n\n- " + tc.blocker + "\n"
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks, waiting)
			m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

			_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			finished := taskFinishedFrom(t, cmd)

			require.NoError(t, finished.Err)
			require.Equal(t, tc.url, openedUrl())
			require.Equal(t, 3, currQueueIssue(t, m).Number)
		})
	}
}

func TestQueueView_DependencyKeysDoNothingWithoutAHighlightedNode(t *testing.T) {
	tests := []struct {
		name   string
		issues []data.IssueData
		keys   []tea.KeyPressMsg
	}{
		{
			name:   "no tree nodes",
			issues: []data.IssueData{queueIssue(5, "")},
			keys: []tea.KeyPressMsg{
				{Code: tea.KeyTab}, {Code: tea.KeyEnter},
			},
		},
		{name: "nothing highlighted", issues: chainedIssues(), keys: []tea.KeyPressMsg{
			{Code: tea.KeyEnter},
		}},
		{name: "preview closed", issues: chainedIssues(), keys: []tea.KeyPressMsg{
			{Text: "p"}, {Code: tea.KeyTab}, {Code: tea.KeyEnter},
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks, tc.issues...)
			selected := currQueueIssue(t, m).Number

			for _, key := range tc.keys {
				m.Update(key)
			}

			require.Empty(t, highlightedLine(t, m))
			require.NotContains(t, startedTasks, "Opening in browser")
			require.Equal(t, 0, m.currSectionId)
			require.Equal(t, selected, currQueueIssue(t, m).Number)
		})
	}
}

func TestHelp_ListsTheDependencyKeysOnlyInTheQueueView(t *testing.T) {
	tests := []struct {
		view  config.ViewType
		lists bool
	}{
		{view: config.QueueView, lists: true},
		{view: config.IssuesView, lists: false},
	}

	for _, tc := range tests {
		t.Run(string(tc.view), func(t *testing.T) {
			var startedTasks []string
			m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
			m.ctx.View = tc.view
			zone.NewGlobal()
			zone.SetEnabled(false)
			m.Update(tea.WindowSizeMsg{Width: 400, Height: 80})

			m.Update(tea.KeyPressMsg{Text: "?"})

			help := ansi.Strip(m.footer.View())
			require.Contains(t, help, "switch to PRs")
			for _, binding := range []string{`tab\s+next dependency`, `enter\s+jump to dependency`} {
				if tc.lists {
					require.Regexp(t, binding, help)
				} else {
					require.NotRegexp(t, binding, help)
				}
			}
		})
	}
}

func TestQueueView_DependencyKeysCanBeRebound(t *testing.T) {
	issueKeys := keys.IssueKeys
	t.Cleanup(func() { keys.IssueKeys = issueKeys })
	require.NoError(t, keys.Rebind(nil, []config.Keybinding{
		{Key: "b", Builtin: "nextDependency"},
		{Key: "J", Builtin: "jumpToDependency"},
	}, nil, nil, nil))
	var startedTasks []string
	m := newQueueTestModel(t, &startedTasks, chainedIssues()...)
	m.Update(tea.KeyPressMsg{Text: "j"})

	m.Update(tea.KeyPressMsg{Text: "b"})
	m.Update(tea.KeyPressMsg{Text: "J"})

	require.Equal(t, 2, currQueueIssue(t, m).Number)
}
