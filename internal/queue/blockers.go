package queue

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Ref names an issue. Owner and Repo are empty for an issue of the launch repo.
type Ref struct {
	Owner  string
	Repo   string
	Number int
}

func (r Ref) String() string {
	if r.Owner == "" {
		return fmt.Sprintf("#%d", r.Number)
	}
	return fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.Number)
}

const repoPattern = `([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)`

var (
	noBlockers = regexp.MustCompile(`(?i)None\s*-\s*can start immediately`)
	issueRef   = regexp.MustCompile(
		`https://github\.com/` + repoPattern + `/issues/(\d+)|` + repoPattern + `#(\d+)|#(\d+)`,
	)
)

// Blockers reads the issues listed under the body's "## Blocked by" heading,
// mirroring wiq_blocked_by_section and wiq_blockers in wiq-lib.sh.
func Blockers(body, launchOwner, launchRepo string) []Ref {
	section := blockedBySection(body)
	if noBlockers.MatchString(section) {
		return nil
	}

	var refs []Ref
	for _, m := range issueRef.FindAllStringSubmatch(section, -1) {
		ref := parseRef(m)
		if strings.EqualFold(ref.Owner, launchOwner) && strings.EqualFold(ref.Repo, launchRepo) {
			ref = Ref{Number: ref.Number}
		}
		if !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	return refs
}

func parseRef(match []string) Ref {
	switch {
	case match[3] != "":
		return Ref{Owner: match[1], Repo: match[2], Number: atoi(match[3])}
	case match[6] != "":
		return Ref{Owner: match[4], Repo: match[5], Number: atoi(match[6])}
	default:
		return Ref{Number: atoi(match[7])}
	}
}

func atoi(digits string) int {
	n, _ := strconv.Atoi(digits)
	return n
}

func blockedBySection(body string) string {
	var section []string
	collecting := false
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r", ""), "\n") {
		switch {
		case strings.HasPrefix(line, "## Blocked by"):
			collecting = true
		case strings.HasPrefix(line, "## "):
			collecting = false
		case collecting:
			section = append(section, line)
		}
	}
	return strings.Join(section, "\n")
}
