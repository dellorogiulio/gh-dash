package data

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// IssueRef names an issue. Owner and Repo are empty for an issue of the launch repo.
type IssueRef struct {
	Owner  string
	Repo   string
	Number int
}

func (r IssueRef) String() string {
	if r.Owner == "" {
		return fmt.Sprintf("#%d", r.Number)
	}
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

// FetchIssueStates looks up the state of every ref, issue or pull request, in
// a single request. Refs that do not resolve are missing from the map, and
// the resolved ones are returned even when the error is non-nil. A number
// GraphQL's Int cannot carry would fail the whole request, so such refs are
// left out of it and stay unresolved.
func FetchIssueStates(refs []IssueRef) (map[IssueRef]string, error) {
	refs = slices.DeleteFunc(slices.Clone(refs), func(r IssueRef) bool {
		return r.Number < 1 || r.Number > math.MaxInt32
	})
	states := map[IssueRef]string{}
	if len(refs) == 0 {
		return states, nil
	}
	if err := ensureClient(); err != nil {
		return states, err
	}

	params := []string{}
	fields := []string{}
	variables := map[string]any{}
	for i, ref := range refs {
		params = append(
			params,
			fmt.Sprintf("$owner%d: String!, $name%d: String!, $number%d: Int!", i, i, i),
		)
		fields = append(fields, fmt.Sprintf(
			"%s: repository(owner: $owner%d, name: $name%d) "+
				"{ issueOrPullRequest(number: $number%d) "+
				"{ ... on Issue { state } ... on PullRequest { state } } }",
			refAlias(i), i, i, i,
		))
		variables[fmt.Sprintf("owner%d", i)] = ref.Owner
		variables[fmt.Sprintf("name%d", i)] = ref.Repo
		variables[fmt.Sprintf("number%d", i)] = ref.Number
	}
	query := fmt.Sprintf(
		"query IssueStates(%s) { %s }",
		strings.Join(params, ", "),
		strings.Join(fields, " "),
	)

	var response map[string]*struct {
		IssueOrPullRequest *struct{ State string }
	}
	err := client.Do(query, variables, &response)
	for i, ref := range refs {
		if repo := response[refAlias(i)]; repo != nil && repo.IssueOrPullRequest != nil {
			states[ref] = repo.IssueOrPullRequest.State
		}
	}
	return states, err
}

func refAlias(i int) string {
	return fmt.Sprintf("ref%d", i)
}
