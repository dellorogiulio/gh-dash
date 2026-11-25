package queuesection

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
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

func TestQueueRowsShowStates(t *testing.T) {
	ctx := newTestContext(t, "me")
	m := new(NewModel(0, ctx, time.Now(), time.Now()))
	withBlockers := func(number int, blockers string) data.IssueData {
		i := issue(number, "")
		i.Body += "\n\n## Blocked by\n\n" + blockers + "\n"
		return i
	}

	receiveOpenIssues(m, []data.IssueData{
		withBlockers(2, "- #5\n- #8\n- other/lib#3"),
		withBlockers(3, "None - can start immediately"),
		withBlockers(5, "- #1"),
		withBlockers(6, "- #4"),
		{Number: 8, Title: "not a candidate"},
	})

	require.Equal(t, []int{3, 5, 6, 2}, rowNumbers(m))
	require.Equal(t, []string{
		"🟢 Next",
		"🟢 Ready",
		"🟢 Ready",
		"🔴 Blocked by #5, #8, other/lib#3",
	}, stateTexts(m))
}

type handlerTransport http.HandlerFunc

func (h handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h(w, req)
	resp := w.Result()
	resp.Request = req
	return resp, nil
}

// serveGitHub answers the open issues query with issues, and the batched
// state query with statesResponse; it returns how many state queries came in.
func serveGitHub(
	t *testing.T,
	issues []data.IssueData,
	statesResponse http.HandlerFunc,
) *int {
	t.Helper()
	stateQueries := 0
	c, err := gh.NewGraphQLClient(gh.ClientOptions{
		Host:      "localhost:3000",
		AuthToken: "fake-token",
		Transport: handlerTransport(func(w http.ResponseWriter, req *http.Request) {
			raw, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var body struct{ Query string }
			require.NoError(t, json.Unmarshal(raw, &body))
			if strings.Contains(body.Query, "IssueStates") {
				stateQueries++
				req.Body = io.NopCloser(bytes.NewReader(raw))
				statesResponse(w, req)
				return
			}
			nodes := []map[string]any{}
			for _, i := range issues {
				nodes = append(
					nodes,
					map[string]any{"number": i.Number, "title": i.Title, "body": i.Body},
				)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"repository": map[string]any{
				"issues": map[string]any{"nodes": nodes, "pageInfo": map[string]any{}},
			}}})
		}),
	})
	require.NoError(t, err)
	data.SetClient(c)
	t.Cleanup(func() { data.SetClient(nil) })
	return &stateQueries
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
			stateQueries := serveGitHub(t, issues, tc.statesResponse)
			m := new(NewModel(0, newTestContext(t, "me"), time.Now(), time.Now()))

			fetch(t, m)

			require.Equal(t, tc.expected, stateTexts(m))
			require.Equal(t, 1, *stateQueries)
		})
	}
}

func TestQueueWithoutBlockersInOtherReposSkipsTheStateQuery(t *testing.T) {
	blockedHere := issue(2, "")
	blockedHere.Body += "\n\n## Blocked by\n\n- #3\n"
	stateQueries := serveGitHub(t, []data.IssueData{blockedHere, issue(3, "")}, answerState("OPEN"))
	m := new(NewModel(0, newTestContext(t, "me"), time.Now(), time.Now()))

	fetch(t, m)

	require.Equal(t, []string{"🟢 Next", "🔴 Blocked by #3"}, stateTexts(m))
	require.Zero(t, *stateQueries)
}

func TestQueueKeepsResolvedBlockersWhenOthersFail(t *testing.T) {
	blockedBy := func(number int, blockers string) data.IssueData {
		i := issue(number, "")
		i.Body += "\n\n## Blocked by\n\n" + blockers + "\n"
		return i
	}
	issues := []data.IssueData{
		blockedBy(2, "- other/lib#1"),
		blockedBy(3, "- other/lib#1"),
		blockedBy(4, "- other/lib#2"),
	}
	var requested []string
	stateQueries := serveGitHub(t, issues, func(w http.ResponseWriter, req *http.Request) {
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
				repos[alias] = map[string]any{"issueOrPullRequest": map[string]any{"state": "CLOSED"}}
				continue
			}
			repos[alias] = nil
			errs = append(errs, map[string]any{"message": "not found", "path": []string{alias}})
		}
		writeJSON(t, w, map[string]any{"data": repos, "errors": errs})
	})
	m := new(NewModel(0, newTestContext(t, "me"), time.Now(), time.Now()))

	fetch(t, m)

	require.Equal(t, []string{"other/lib#1", "other/lib#2"}, requested)
	require.Equal(t, 1, *stateQueries)
	require.Equal(t, []int{2, 3, 4}, rowNumbers(m))
	require.Equal(t, []string{"🟢 Next", "🟢 Ready", "🔴 Blocked by other/lib#2"}, stateTexts(m))
}

func TestQueueLooksUpValidBlockersNextToAnOversizedNumber(t *testing.T) {
	blockedBy := func(number int, blockers string) data.IssueData {
		i := issue(number, "")
		i.Body += "\n\n## Blocked by\n\n" + blockers + "\n"
		return i
	}
	issues := []data.IssueData{
		blockedBy(2, "- other/lib#99999999999"),
		blockedBy(3, "- other/lib#1"),
	}
	stateQueries := serveGitHub(t, issues, func(w http.ResponseWriter, req *http.Request) {
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
	m := new(NewModel(0, newTestContext(t, "me"), time.Now(), time.Now()))

	fetch(t, m)

	require.Equal(t, []int{3, 2}, rowNumbers(m))
	require.Equal(t, []string{"🟢 Next", "🔴 Blocked by other/lib#99999999999"}, stateTexts(m))
	require.Equal(t, 1, *stateQueries)
}
