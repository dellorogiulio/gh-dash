package data

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gh "github.com/cli/go-gh/v2/pkg/api"
	"github.com/stretchr/testify/require"
)

type handlerTransport struct {
	handler http.HandlerFunc
}

func (h handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	h.handler(w, req)
	resp := w.Result()
	resp.Request = req
	return resp, nil
}

func useFakeClient(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	original := client
	t.Cleanup(func() { SetClient(original) })
	c, err := gh.NewGraphQLClient(gh.ClientOptions{
		Host:      "localhost:3000",
		AuthToken: "fake-token",
		Transport: handlerTransport{handler: handler},
	})
	require.NoError(t, err)
	SetClient(c)
}

type openIssuesPage struct {
	numbers    []int
	nextCursor string
}

func serveOpenIssuesPages(t *testing.T, pages map[string]openIssuesPage) *[]string {
	t.Helper()
	var requested []string
	useFakeClient(t, func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Variables struct {
				Owner  string
				Name   string
				Cursor *string
			}
		}
		require.NoError(t, json.NewDecoder(req.Body).Decode(&body))
		require.Equal(t, "owner", body.Variables.Owner)
		require.Equal(t, "repo", body.Variables.Name)

		cursor := ""
		if body.Variables.Cursor != nil {
			cursor = *body.Variables.Cursor
		}
		requested = append(requested, cursor)
		page := pages[cursor]

		nodes := []map[string]any{}
		for _, n := range page.numbers {
			nodes = append(nodes, map[string]any{"number": n})
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"repository": map[string]any{"issues": map[string]any{
				"nodes": nodes,
				"pageInfo": map[string]any{
					"hasNextPage": page.nextCursor != "",
					"endCursor":   page.nextCursor,
				},
			}}},
		}))
	})

	return &requested
}

func TestFetchAllOpenIssues_PagesUntilNoNextPage(t *testing.T) {
	requested := serveOpenIssuesPages(t, map[string]openIssuesPage{
		"":   {numbers: []int{1, 2}, nextCursor: "c1"},
		"c1": {numbers: []int{3}, nextCursor: "c2"},
		"c2": {numbers: []int{4}},
	})

	issues, err := FetchAllOpenIssues("owner", "repo")
	require.NoError(t, err)

	var numbers []int
	for _, i := range issues {
		numbers = append(numbers, i.Number)
	}
	require.Equal(t, []int{1, 2, 3, 4}, numbers)
	require.Equal(t, []string{"", "c1", "c2"}, *requested)
}
