package queue

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
)

func TestBlockers(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected []Ref
	}{
		{
			name:     "wiq fixture issue 3 lists its numbers",
			body:     "## Parent\n\n#1\n\n## Blocked by\n\n- #2\n- #5\n",
			expected: []Ref{{Number: 2}, {Number: 5}},
		},
		{
			name: "wiq fixture 'None - can start immediately' lists none",
			body: "## Parent\n\n#1\n\n## Blocked by\n\nNone - can start immediately\n",
		},
		{
			name: "wiq cross-repo fixture keeps other repos' refs",
			body: "## Blocked by\n\n- #178\n- Sesamo-srl/sesamo.quinoa#85\n" +
				"- https://github.com/Sesamo-srl/sesamo.website_chatbot/issues/279\n",
			expected: []Ref{
				{Number: 178},
				{Owner: "Sesamo-srl", Repo: "sesamo.quinoa", Number: 85},
				{Owner: "Sesamo-srl", Repo: "sesamo.website_chatbot", Number: 279},
			},
		},
		{
			name:     "the section ends at the next ## heading",
			body:     "## Blocked by\n\n- #2\n\n## Notes\n\nsee #9\n",
			expected: []Ref{{Number: 2}},
		},
		{
			name:     "refs before the section are ignored",
			body:     "## Parent\n\n#1\n\n## Blocked by\n\n- #2\n",
			expected: []Ref{{Number: 2}},
		},
		{
			name:     "a ### subheading does not end the section",
			body:     "## Blocked by\n\n### Code\n\n- #2\n\n### Design\n\n- #3\n",
			expected: []Ref{{Number: 2}, {Number: 3}},
		},
		{
			name:     "a second ## Blocked by heading collects again",
			body:     "## Blocked by\n\n- #2\n\n## Notes\n\n#7\n\n## Blocked by\n\n- #4\n",
			expected: []Ref{{Number: 2}, {Number: 4}},
		},
		{
			name: "'none - Can Start Immediately' is matched case-insensitively and loosely spaced",
			body: "## Blocked by\n\nnone  -Can Start Immediately (was #4)\n",
		},
		{
			name:     "a ref in prose counts",
			body:     "## Blocked by\n\nWaits on #12 landing first.\n",
			expected: []Ref{{Number: 12}},
		},
		{
			name:     "a URL with a fragment gives its issue only",
			body:     "## Blocked by\n\n- https://github.com/other/lib/issues/3#issuecomment-1\n",
			expected: []Ref{{Owner: "other", Repo: "lib", Number: 3}},
		},
		{
			name: "launch repo URL and owner/repo#N collapse onto #N",
			body: "## Blocked by\n\n- https://github.com/Owner/Repo/issues/2\n- owner/REPO#3\n" +
				"- #2\n- #3\n",
			expected: []Ref{{Number: 2}, {Number: 3}},
		},
		{
			name:     "duplicates are listed once",
			body:     "## Blocked by\n\n- #2\n- #5\n- #2\n- other/lib#1\n- other/lib#1\n",
			expected: []Ref{{Number: 2}, {Number: 5}, {Owner: "other", Repo: "lib", Number: 1}},
		},
		{
			name:     "CRLF bodies parse",
			body:     "## Blocked by\r\n\r\n- #2\r\n\r\n## Notes\r\n#9\r\n",
			expected: []Ref{{Number: 2}},
		},
		{
			name: "CRLF 'None - can start immediately' lists none",
			body: "## Blocked by\r\n\r\nNone - can start immediately\r\n",
		},
		{
			name: "no Blocked by section lists none",
			body: "## Parent\n\n#1\n",
		},
		{
			name:     "a number beyond 32 bits is kept",
			body:     "## Blocked by\n\n- other/lib#99999999999\n",
			expected: []Ref{{Owner: "other", Repo: "lib", Number: 99999999999}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, Blockers(tt.body, "owner", "repo"))
		})
	}
}

func TestBlockersKeepANumberBeyond64BitsOutOfLookupRange(t *testing.T) {
	refs := Blockers("## Blocked by\n\n- other/lib#123456789012345678901234567890\n", "owner", "repo")

	require.Len(t, refs, 1)
	require.Equal(t, "other/lib", refs[0].Owner+"/"+refs[0].Repo)
	require.False(t, 1 <= refs[0].Number && refs[0].Number <= math.MaxInt32)
}

func TestOtherRepoBlockers(t *testing.T) {
	blockedBy := func(blockers string) data.IssueData {
		return data.IssueData{Body: "## Blocked by\n\n" + blockers + "\n"}
	}
	issues := []data.IssueData{
		blockedBy("- #2\n- other/lib#1"),
		blockedBy("- https://github.com/Owner/Repo/issues/3\n- other/lib#1\n- other/app#7"),
		blockedBy("None - can start immediately"),
		blockedBy("- owner/repo#4\n- https://github.com/other/app/issues/7"),
	}

	require.Equal(t, []Ref{
		{Owner: "other", Repo: "lib", Number: 1},
		{Owner: "other", Repo: "app", Number: 7},
	}, OtherRepoBlockers(issues, "owner", "repo"))
}
