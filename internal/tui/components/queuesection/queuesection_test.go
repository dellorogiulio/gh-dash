package queuesection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
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

func newTabs(t *testing.T, user string) (queueTab, needsMeTab *Model) {
	t.Helper()
	tabs := InitSections(newTestContext(t, user))
	require.Len(t, tabs, 2)
	return tabs[0].(*Model), tabs[1].(*Model)
}

func newQueueTab(ctx *context.ProgramContext) *Model {
	return InitSections(ctx)[0].(*Model)
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
			expectedFirst: []int{2, 5, 9, 4},
		},
		{
			name:          "login arrives after the issues",
			loginAtFetch:  "",
			expectedFirst: []int{2, 9, 4},
			loginLater:    "me",
			expectedLater: []int{2, 5, 9, 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newTestContext(t, tc.loginAtFetch)
			m := newQueueTab(ctx)

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
	m := newQueueTab(ctx)

	require.Nil(t, m.FetchNextPageSectionRows())
	require.False(t, m.GetIsLoading())
	require.Zero(t, m.NumRows())
}

func TestQueueRowsShowStates(t *testing.T) {
	ctx := newTestContext(t, "me")
	m := newQueueTab(ctx)

	receiveOpenIssues(m, []data.IssueData{
		blockedBy(2, "- #5\n- #8\n- other/lib#3"),
		blockedBy(3, "None - can start immediately"),
		blockedBy(5, "- #1"),
		blockedBy(6, "- #4"),
		{Number: 8, Title: "not a candidate"},
		issue(9, "", "blocked"),
	})

	require.Equal(t, []int{3, 5, 6, 2, 9}, rowNumbers(m))
	require.Equal(t, []string{
		"🟢 Next",
		"🟢 Ready",
		"🟢 Ready",
		"🔴 Blocked by #5, #8, other/lib#3",
		"🟠 Parked",
	}, stateTexts(m))
}

func TestQueueBlockerListsMarkHITLAndParkedBlockers(t *testing.T) {
	m := newQueueTab(newTestContext(t, "me"))

	receiveOpenIssues(m, []data.IssueData{
		blockedBy(2, "- #4\n- #9\n- #7"),
		issue(4, "", "hitl"),
		issue(7, "someone-else"),
		issue(9, "", "blocked"),
	})

	require.Equal(t, "🔴 Blocked by #4 (HITL), #9 (parked), #7", stateTexts(m)[0])
}

func blockedBy(number int, blockers string) data.IssueData {
	i := issue(number, "")
	i.Body += "\n\n## Blocked by\n\n" + blockers + "\n"
	return i
}

func columnTexts(m *Model, column int) []string {
	texts := []string{}
	for _, row := range m.Table.Rows {
		texts = append(texts, ansi.Strip(row[column]))
	}
	return texts
}

func TestQueueRowsShowHowManyIssuesTheyUnblock(t *testing.T) {
	m := newQueueTab(newTestContext(t, "me"))

	receiveOpenIssues(m, []data.IssueData{
		issue(189, ""),
		blockedBy(195, "- #189"),
		blockedBy(196, "- #195"),
		blockedBy(300, "- #301"),
		blockedBy(301, "- #300"),
	})

	require.Equal(t, []int{189, 195, 196, 300, 301}, rowNumbers(m))
	require.Equal(t, []string{"2", "1", "0", "1", "1"}, columnTexts(m, 1))
}

func TestNeedsMeTabListsParkedAndBlockingHITLIssuesNextToTheQueue(t *testing.T) {
	serveGitHub(t, []data.IssueData{
		issue(3, ""),
		blockedBy(2, "- #4\n- #6"),
		issue(4, "", "blocked"),
		issue(5, "", "blocked"),
		issue(6, "", "hitl"),
		issue(7, "", "hitl"),
		issue(189, "", "hitl"),
		blockedBy(195, "- #189"),
		blockedBy(196, "- #195"),
		blockedBy(300, "- #301"),
		blockedBy(301, "- #300"),
	}, answerState("OPEN"))
	queueTab, needsMeTab := newTabs(t, "me")

	fetch(t, queueTab)

	require.Equal(t, []int{3, 2, 195, 196, 300, 301, 4, 5}, rowNumbers(queueTab))
	require.Equal(t, []string{"0", "0", "1", "0", "1", "1", "1", "0"}, columnTexts(queueTab, 1))
	require.Equal(t, []int{189, 4, 6, 5}, rowNumbers(needsMeTab))
	require.Equal(t,
		[]string{"🟣 HITL", "🟠 Parked", "🟣 HITL", "🟠 Parked"},
		stateTexts(needsMeTab),
	)
	require.Equal(t, []string{"2", "1", "1", "0"}, columnTexts(needsMeTab, 1))
}

func TestRefreshFetchesOnceForBothTabs(t *testing.T) {
	tests := []struct {
		name    string
		refresh func(queueTab, needsMeTab *Model) tea.Cmd
	}{
		{
			name: "all tabs",
			refresh: func(queueTab, needsMeTab *Model) tea.Cmd {
				return FetchAllSections(queueTab.Ctx, []section.Section{queueTab, needsMeTab})
			},
		},
		{
			name: "from the Needs me tab",
			refresh: func(_, needsMeTab *Model) tea.Cmd {
				needsMeTab.ResetRows()
				return tea.Batch(needsMeTab.FetchNextPageSectionRows()...)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queries := serveGitHub(t, []data.IssueData{
				blockedBy(2, "- other/lib#1"),
				issue(4, "", "blocked"),
			}, answerState("OPEN"))
			queueTab, needsMeTab := newTabs(t, "me")
			tabs := []*Model{queueTab, needsMeTab}

			finished := taskFinishedFrom(t, tc.refresh(queueTab, needsMeTab))
			require.True(t, queueTab.GetIsLoading())
			require.True(t, needsMeTab.GetIsLoading())
			tabs[finished.SectionId].Update(finished.Msg)

			require.Equal(t, queryCounts{openIssues: 1, states: 1}, *queries)
			require.False(t, queueTab.GetIsLoading())
			require.False(t, needsMeTab.GetIsLoading())
			require.Equal(t, []int{2, 4}, rowNumbers(queueTab))
			require.Equal(t, []int{4}, rowNumbers(needsMeTab))
		})
	}
}

// taskFinishedFrom runs cmd, and the commands it batches, until one reports
// a finished task.
func taskFinishedFrom(t *testing.T, cmd tea.Cmd) constants.TaskFinishedMsg {
	t.Helper()
	var finished []constants.TaskFinishedMsg
	pending := []tea.Cmd{cmd}
	for len(pending) > 0 {
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case tea.BatchMsg:
			pending = append(pending, msg...)
		case constants.TaskFinishedMsg:
			finished = append(finished, msg)
		}
	}
	require.Len(t, finished, 1)
	return finished[0]
}

type handlerTransport http.HandlerFunc

func (h handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h(w, req)
	resp := w.Result()
	resp.Request = req
	return resp, nil
}

type queryCounts struct {
	openIssues, states int
}

// serveGitHub answers the open issues query with issues, and the batched
// state query with statesResponse; it counts the queries that came in.
func serveGitHub(
	t *testing.T,
	issues []data.IssueData,
	statesResponse http.HandlerFunc,
) *queryCounts {
	t.Helper()
	queries := &queryCounts{}
	c, err := gh.NewGraphQLClient(gh.ClientOptions{
		Host:      "localhost:3000",
		AuthToken: "fake-token",
		Transport: handlerTransport(func(w http.ResponseWriter, req *http.Request) {
			raw, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var body struct{ Query string }
			require.NoError(t, json.Unmarshal(raw, &body))
			if strings.Contains(body.Query, "IssueStates") {
				queries.states++
				req.Body = io.NopCloser(bytes.NewReader(raw))
				statesResponse(w, req)
				return
			}
			queries.openIssues++
			nodes := []map[string]any{}
			for _, i := range issues {
				nodes = append(nodes, map[string]any{
					"number":    i.Number,
					"title":     i.Title,
					"body":      i.Body,
					"labels":    i.Labels,
					"assignees": i.Assignees,
				})
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{
				"issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{}},
			}}})
		}),
	})
	require.NoError(t, err)
	data.SetClient(c)
	t.Cleanup(func() { data.SetClient(nil) })
	return queries
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func answerState(state string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"ref0": map[string]any{"issueOrPullRequest": map[string]any{"state": state}},
		}})
	}
}

func fetch(t *testing.T, m *Model) {
	t.Helper()
	cmds := m.FetchNextPageSectionRows()
	finished, ok := cmds[1]().(constants.TaskFinishedMsg)
	require.True(t, ok)
	require.NoError(t, finished.Err)
	m.Update(finished.Msg)
}

func stateTexts(m *Model) []string {
	states := []string{}
	for _, row := range m.Table.Rows {
		states = append(states, ansi.Strip(row[0]))
	}
	return states
}

func TestQueueResolvesBlockersInOtherRepos(t *testing.T) {
	blockedElsewhere := issue(2, "")
	blockedElsewhere.Body += "\n\n## Blocked by\n\n- other/lib#1\n"
	free := issue(3, "")

	tests := []struct {
		name           string
		statesResponse http.HandlerFunc
		expected       []string
	}{
		{
			name:           "a closed blocker frees the candidate",
			statesResponse: answerState("CLOSED"),
			expected:       []string{"🟢 Next", "🟢 Ready"},
		},
		{
			name:           "an open blocker keeps it Blocked",
			statesResponse: answerState("OPEN"),
			expected:       []string{"🟢 Next", "🔴 Blocked by other/lib#1"},
		},
		{
			name: "an HTTP error keeps it Blocked",
			statesResponse: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			expected: []string{"🟢 Next", "🔴 Blocked by other/lib#1"},
		},
		{
			name: "a GraphQL error keeps it Blocked",
			statesResponse: func(w http.ResponseWriter, _ *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data":   nil,
					"errors": []map[string]any{{"message": "rate limited"}},
				})
			},
			expected: []string{"🟢 Next", "🔴 Blocked by other/lib#1"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			issues := []data.IssueData{blockedElsewhere, free}
			queries := serveGitHub(t, issues, tc.statesResponse)
			m := newQueueTab(newTestContext(t, "me"))

			fetch(t, m)

			require.Equal(t, tc.expected, stateTexts(m))
			require.Equal(t, 1, queries.states)
		})
	}
}

func TestQueueWithoutBlockersInOtherReposSkipsTheStateQuery(t *testing.T) {
	blockedHere := issue(2, "")
	blockedHere.Body += "\n\n## Blocked by\n\n- #3\n"
	queries := serveGitHub(t, []data.IssueData{blockedHere, issue(3, "")}, answerState("OPEN"))
	m := newQueueTab(newTestContext(t, "me"))

	fetch(t, m)

	require.Equal(t, []string{"🟢 Next", "🔴 Blocked by #3"}, stateTexts(m))
	require.Zero(t, queries.states)
}

func TestQueueKeepsResolvedBlockersWhenOthersFail(t *testing.T) {
	issues := []data.IssueData{
		blockedBy(2, "- other/lib#1"),
		blockedBy(3, "- other/lib#1"),
		blockedBy(4, "- other/lib#2"),
	}
	var requested []string
	queries := serveGitHub(t, issues, func(w http.ResponseWriter, req *http.Request) {
		var body struct{ Variables map[string]any }
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		repos := map[string]any{}
		errs := []map[string]any{}
		for i := 0; body.Variables[fmt.Sprintf("number%d", i)] != nil; i++ {
			number := body.Variables[fmt.Sprintf("number%d", i)]
			requested = append(requested, fmt.Sprintf("%v/%v#%v",
				body.Variables[fmt.Sprintf("owner%d", i)],
				body.Variables[fmt.Sprintf("name%d", i)],
				number,
			))
			alias := fmt.Sprintf("ref%d", i)
			if number == float64(1) {
				repos[alias] = map[string]any{
					"issueOrPullRequest": map[string]any{"state": "CLOSED"},
				}
				continue
			}
			repos[alias] = nil
			errs = append(errs, map[string]any{"message": "not found", "path": []string{alias}})
		}
		writeJSON(t, w, map[string]any{"data": repos, "errors": errs})
	})
	m := newQueueTab(newTestContext(t, "me"))

	fetch(t, m)

	require.Equal(t, []string{"other/lib#1", "other/lib#2"}, requested)
	require.Equal(t, 1, queries.states)
	require.Equal(t, []int{2, 3, 4}, rowNumbers(m))
	require.Equal(t, []string{"🟢 Next", "🟢 Ready", "🔴 Blocked by other/lib#2"}, stateTexts(m))
}

func TestQueueLooksUpValidBlockersNextToAnOversizedNumber(t *testing.T) {
	issues := []data.IssueData{
		blockedBy(2, "- other/lib#99999999999"),
		blockedBy(3, "- other/lib#1"),
	}
	queries := serveGitHub(t, issues, func(w http.ResponseWriter, req *http.Request) {
		var body struct{ Variables map[string]any }
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		repos := map[string]any{}
		for i := 0; body.Variables[fmt.Sprintf("number%d", i)] != nil; i++ {
			if body.Variables[fmt.Sprintf("number%d", i)].(float64) > math.MaxInt32 {
				writeJSON(t, w, map[string]any{
					"errors": []map[string]any{{"message": "Variable has an invalid value"}},
				})
				return
			}
			repos[fmt.Sprintf("ref%d", i)] = map[string]any{
				"issueOrPullRequest": map[string]any{"state": "CLOSED"},
			}
		}
		writeJSON(t, w, map[string]any{"data": repos})
	})
	m := newQueueTab(newTestContext(t, "me"))

	fetch(t, m)

	require.Equal(t, []int{3, 2}, rowNumbers(m))
	require.Equal(t, []string{"🟢 Next", "🔴 Blocked by other/lib#99999999999"}, stateTexts(m))
	require.Equal(t, 1, queries.states)
}

// newRepoDir creates a checkout folder named repo, with a wiq worktree folder
// next to it for each issue number.
func newRepoDir(t *testing.T, worktreeIssues ...int) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	require.NoError(t, os.Mkdir(repo, 0o755))
	for _, n := range worktreeIssues {
		addWorktree(t, repo, n)
	}
	return repo
}

func addWorktree(t *testing.T, repo string, issue int) {
	t.Helper()
	require.NoError(t, os.Mkdir(fmt.Sprintf("%s.wiq-issue-%d", repo, issue), 0o755))
}

func worktreeMarkers(m *Model) map[int]string {
	markers := map[int]string{}
	numbers := rowNumbers(m)
	for i, marker := range columnTexts(m, 2) {
		markers[numbers[i]] = marker
	}
	return markers
}

var issuesForWorktrees = []data.IssueData{
	blockedBy(2, "- #3"),
	issue(3, "", "blocked"),
}

func TestRowsMarkIssuesWithAWiqWorktree(t *testing.T) {
	serveGitHub(t, issuesForWorktrees, answerState("OPEN"))
	queueTab, needsMeTab := newTabs(t, "me")
	queueTab.Ctx.RepoPath = newRepoDir(t, 3)

	fetch(t, queueTab)

	require.Equal(t, map[int]string{2: "", 3: "⏵ worktree"}, worktreeMarkers(queueTab))
	require.Equal(t, map[int]string{3: "⏵ worktree"}, worktreeMarkers(needsMeTab))
}

func TestRowsWithoutALocalRepoPathHaveNoWorktreeMarker(t *testing.T) {
	serveGitHub(t, issuesForWorktrees, answerState("OPEN"))
	m := newQueueTab(newTestContext(t, "me"))
	m.Ctx.RepoPath = ""

	fetch(t, m)

	require.Equal(t, map[int]string{2: "", 3: ""}, worktreeMarkers(m))
}

func TestWorktreeMarkerWaitsForTheNextRefresh(t *testing.T) {
	serveGitHub(t, issuesForWorktrees, answerState("OPEN"))
	m := newQueueTab(newTestContext(t, "me"))
	m.Ctx.RepoPath = newRepoDir(t)
	fetch(t, m)

	addWorktree(t, m.Ctx.RepoPath, 2)
	for range 3 {
		m.Update(nil)
		m.Table.SetRows(m.BuildRows())
	}
	require.Equal(t, "", worktreeMarkers(m)[2])

	m.ResetRows()
	fetch(t, m)
	require.Equal(t, "⏵ worktree", worktreeMarkers(m)[2])
}

func TestWorktreesAreLookedUpNextToTheRepoPathsCheckoutFirst(t *testing.T) {
	serveGitHub(t, issuesForWorktrees, answerState("OPEN"))
	m := newQueueTab(newTestContext(t, "me"))
	m.Ctx.Config.RepoPaths = map[string]string{"owner/repo": newRepoDir(t, 3) + "/"}
	m.Ctx.RepoPath = newRepoDir(t, 2)

	fetch(t, m)

	require.Equal(t, map[int]string{2: "", 3: "⏵ worktree"}, worktreeMarkers(m))
}

func TestWorktreesAreFoundForARepoPathUnderTheHomeFolder(t *testing.T) {
	serveGitHub(t, issuesForWorktrees, answerState("OPEN"))
	repo := newRepoDir(t, 3)
	t.Setenv("HOME", filepath.Dir(repo))
	m := newQueueTab(newTestContext(t, "me"))
	m.Ctx.Config.RepoPaths = map[string]string{"owner/*": "~/*"}

	fetch(t, m)

	require.Equal(t, "⏵ worktree", worktreeMarkers(m)[3])
}

func renderedRows(t *testing.T, compact bool) map[int]string {
	t.Helper()
	m := newQueueTab(newTestContext(t, "me"))
	m.Ctx.Config.Theme.Ui.Table.Compact = compact
	m.Ctx.MainContentWidth, m.Ctx.MainContentHeight = 160, 40
	m.UpdateProgramContext(m.Ctx)
	m.FetchNextPageSectionRows()
	m.Update(SectionQueueFetchedMsg{
		Issues: []data.IssueData{
			blockedBy(2, "- #4\n- #9"),
			blockedBy(3, "- #4\n- #9"),
			issue(4, "", "hitl"),
			issue(9, "", "blocked"),
		},
		Worktrees: map[int]bool{2: true},
		TaskId:    m.LastFetch.TaskId,
	})

	rows := map[int]string{}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i, line := range lines {
		for _, number := range []int{2, 3} {
			if strings.Contains(line, fmt.Sprintf("#%d an issue", number)) {
				rows[number] = strings.Join(lines[i:min(i+2, len(lines))], "\n")
				if compact {
					rows[number] = line
				}
			}
		}
	}
	require.Len(t, rows, 2)
	return rows
}

func TestRenderedRowsShowTheWorktreeMarkerWithoutHidingTheState(t *testing.T) {
	tests := map[bool][]string{
		true:  {"🔴 Blocked by #4"},
		false: {"🔴 Blocked by #4", "#9 (parked)"},
	}
	for compact, stateLines := range tests {
		t.Run(fmt.Sprintf("compact=%v", compact), func(t *testing.T) {
			rows := renderedRows(t, compact)

			require.Contains(t, rows[2], "⏵ worktree")
			for _, line := range stateLines {
				require.Contains(t, rows[2], line)
			}
			require.NotContains(t, rows[3], "⏵")
			unmarked := strings.ReplaceAll(rows[2], "⏵ worktree", "          ")
			require.Equal(t, rows[3], strings.ReplaceAll(unmarked, "#2 an issue", "#3 an issue"))
		})
	}
}
