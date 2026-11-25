package data

import (
	"fmt"
	"time"

	"charm.land/log/v2"
	gh "github.com/cli/go-gh/v2/pkg/api"
	graphql "github.com/cli/shurcooL-graphql"

	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type IssueData struct {
	Number int
	Title  string
	Body   string
	State  string
	Author struct {
		Login string
	}
	AuthorAssociation string
	UpdatedAt         time.Time
	CreatedAt         time.Time
	Url               string
	Repository        Repository
	Assignees         Assignees      `graphql:"assignees(first: 3)"`
	Comments          IssueComments  `graphql:"comments(last: 15)"`
	Reactions         IssueReactions `graphql:"reactions(first: 1)"`
	Labels            IssueLabels    `graphql:"labels(first: 20)"`
	SubIssuesSummary  SubIssuesSummary
}

type SubIssuesSummary struct {
	Total     int
	Completed int
}

type IssueComments struct {
	Nodes      []IssueComment
	TotalCount int
}

type IssueComment struct {
	Author struct {
		Login string
	}
	Body      string
	UpdatedAt time.Time
}

type IssueReactions struct {
	TotalCount int
}

type Label struct {
	Color       string
	Name        string
	Description string
}

type IssueLabels struct {
	Nodes []Label
}

func (data IssueData) GetAuthor(theme theme.Theme, showAuthorIcons bool) string {
	author := data.Author.Login
	if showAuthorIcons {
		author += fmt.Sprintf(" %s", GetAuthorRoleIcon(data.AuthorAssociation, theme))
	}
	return author
}

func (data IssueData) GetTitle() string {
	return data.Title
}

func (data IssueData) GetRepoNameWithOwner() string {
	return data.Repository.NameWithOwner
}

func (data IssueData) GetRepoNameAndOwner() (owner, repoName string) {
	return data.Repository.Owner.Login, data.Repository.Name
}

func (data IssueData) GetNumber() int {
	return data.Number
}

func (data IssueData) GetUrl() string {
	return data.Url
}

func (data IssueData) GetUpdatedAt() time.Time {
	return data.UpdatedAt
}

func (data IssueData) GetCreatedAt() time.Time {
	return data.CreatedAt
}

func makeIssuesQuery(query string) string {
	return fmt.Sprintf("is:issue archived:false %s sort:updated", query)
}

func ensureClient() error {
	if client != nil {
		return nil
	}
	var err error
	client, err = gh.DefaultGraphQLClient()
	return err
}

func FetchIssues(query string, limit int, pageInfo *PageInfo) (IssuesResponse, error) {
	if err := ensureClient(); err != nil {
		return IssuesResponse{}, err
	}

	var queryResult struct {
		Search struct {
			Nodes []struct {
				Issue IssueData `graphql:"... on Issue"`
			}
			IssueCount int
			PageInfo   PageInfo
		} `graphql:"search(type: ISSUE, first: $limit, after: $endCursor, query: $query)"`
	}
	var endCursor *string
	if pageInfo != nil {
		endCursor = &pageInfo.EndCursor
	}
	variables := map[string]any{
		"query":     graphql.String(makeIssuesQuery(query)),
		"limit":     graphql.Int(limit),
		"endCursor": (*graphql.String)(endCursor),
	}
	log.Debug("Fetching issues", "query", query, "limit", limit, "endCursor", endCursor)
	err := client.Query("SearchIssues", &queryResult, variables)
	if err != nil {
		return IssuesResponse{}, err
	}
	log.Info("Successfully fetched issues", "query", query, "count", queryResult.Search.IssueCount)

	issues := make([]IssueData, 0, len(queryResult.Search.Nodes))
	for _, node := range queryResult.Search.Nodes {
		issues = append(issues, node.Issue)
	}

	return IssuesResponse{
		Issues:     issues,
		TotalCount: queryResult.Search.IssueCount,
		PageInfo:   queryResult.Search.PageInfo,
	}, nil
}

func FetchAllOpenIssues(owner, name string) ([]IssueData, error) {
	if err := ensureClient(); err != nil {
		return nil, err
	}

	issues := []IssueData{}
	var cursor *string
	for {
		var queryResult struct {
			Repository struct {
				Issues struct {
					Nodes    []IssueData
					PageInfo PageInfo
				} `graphql:"issues(states: OPEN, first: 100, after: $cursor)"`
			} `graphql:"repository(owner: $owner, name: $name)"`
		}
		variables := map[string]any{
			"owner":  graphql.String(owner),
			"name":   graphql.String(name),
			"cursor": (*graphql.String)(cursor),
		}
		if err := client.Query("RepositoryOpenIssues", &queryResult, variables); err != nil {
			return nil, err
		}
		issues = append(issues, queryResult.Repository.Issues.Nodes...)
		pageInfo := queryResult.Repository.Issues.PageInfo
		if !pageInfo.HasNextPage {
			break
		}
		cursor = &pageInfo.EndCursor
	}
	log.Info("Successfully fetched open issues", "repo", owner+"/"+name, "count", len(issues))

	return issues, nil
}

type IssuesResponse struct {
	Issues     []IssueData
	TotalCount int
	PageInfo   PageInfo
}
