package data

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var repositoryField = regexp.MustCompile(
	`(\w+): repository\(owner: \$(\w+), name: \$(\w+)\) \{ issueOrPullRequest\(number: \$(\w+)\)`,
)

// serveIssueStates answers like GitHub does: a number outside GraphQL's Int
// range fails the whole query, and an unknown issue yields a null field plus
// an error, next to the data of the issues it does know.
func serveIssueStates(t *testing.T, known map[IssueRef]string) *int {
	t.Helper()
	requests := 0
	useFakeClient(t, func(w http.ResponseWriter, req *http.Request) {
		requests++
		var body struct {
			Query     string
			Variables map[string]any
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		w.Header().Set("Content-Type", "application/json")

		for _, v := range body.Variables {
			if n, ok := v.(float64); ok && n > math.MaxInt32 {
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
					"errors": []map[string]any{{"message": "Variable has an invalid value"}},
				}))
				return
			}
		}

		data := map[string]any{}
		errors := []map[string]any{}
		for _, m := range repositoryField.FindAllStringSubmatch(body.Query, -1) {
			alias, owner, name, number := m[1], m[2], m[3], m[4]
			ref := IssueRef{
				Owner:  body.Variables[owner].(string),
				Repo:   body.Variables[name].(string),
				Number: int(body.Variables[number].(float64)),
			}
			state, ok := known[ref]
			if !ok {
				data[alias] = nil
				errors = append(
					errors,
					map[string]any{"message": "not found", "path": []string{alias}},
				)
				continue
			}
			data[alias] = map[string]any{"issueOrPullRequest": map[string]any{"state": state}}
		}
		response := map[string]any{"data": data}
		if len(errors) > 0 {
			response["errors"] = errors
		}
		require.NoError(t, json.NewEncoder(w).Encode(response))
	})
	return &requests
}

var (
	closedIssue = IssueRef{Owner: "other", Repo: "lib", Number: 1}
	openIssue   = IssueRef{Owner: "other", Repo: "lib", Number: 2}
	mergedPR    = IssueRef{Owner: "third", Repo: "app", Number: 7}
	missing     = IssueRef{Owner: "gone", Repo: "repo", Number: 9}
)

func TestFetchIssueStates_OneRequestForAllRefs(t *testing.T) {
	requests := serveIssueStates(t, map[IssueRef]string{
		closedIssue: "CLOSED",
		openIssue:   "OPEN",
		mergedPR:    "MERGED",
	})

	states, err := FetchIssueStates([]IssueRef{closedIssue, openIssue, mergedPR})

	require.NoError(t, err)
	require.Equal(t, map[IssueRef]string{
		closedIssue: "CLOSED",
		openIssue:   "OPEN",
		mergedPR:    "MERGED",
	}, states)
	require.Equal(t, 1, *requests)
}

func TestFetchIssueStates_NoRefsNoRequest(t *testing.T) {
	requests := serveIssueStates(t, nil)

	states, err := FetchIssueStates(nil)

	require.NoError(t, err)
	require.Empty(t, states)
	require.Zero(t, *requests)
}

func TestFetchIssueStates_UnresolvedRefKeepsTheResolvedOnes(t *testing.T) {
	serveIssueStates(t, map[IssueRef]string{closedIssue: "CLOSED"})

	states, err := FetchIssueStates([]IssueRef{closedIssue, missing})

	require.Error(t, err)
	require.Equal(t, map[IssueRef]string{closedIssue: "CLOSED"}, states)
}

func TestFetchIssueStates_LeavesOutNumbersGitHubCannotTake(t *testing.T) {
	requests := serveIssueStates(t, map[IssueRef]string{closedIssue: "CLOSED"})
	oversized := IssueRef{Owner: "other", Repo: "lib", Number: 99999999999}
	zero := IssueRef{Owner: "other", Repo: "lib", Number: 0}

	states, err := FetchIssueStates([]IssueRef{oversized, closedIssue, zero})

	require.NoError(t, err)
	require.Equal(t, map[IssueRef]string{closedIssue: "CLOSED"}, states)
	require.Equal(t, 1, *requests)
}

func TestFetchIssueStates_HTTPErrorResolvesNothing(t *testing.T) {
	useFakeClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	states, err := FetchIssueStates([]IssueRef{closedIssue})

	require.Error(t, err)
	require.Empty(t, states)
}
