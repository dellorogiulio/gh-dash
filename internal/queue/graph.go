package queue

import "github.com/dlvhdr/gh-dash/v4/internal/data"

// Graph links the launch repo's open issues through their "## Blocked by"
// sections.
type Graph struct {
	dependents map[int][]int
	blockers   map[int][]int
}

func newGraph(openIssues []data.IssueData, launchOwner, launchRepo string) Graph {
	open := map[int]bool{}
	for _, i := range openIssues {
		open[i.Number] = true
	}
	g := Graph{dependents: map[int][]int{}, blockers: map[int][]int{}}
	for _, i := range openIssues {
		for _, ref := range Blockers(i.Body, launchOwner, launchRepo) {
			if ref.Owner == "" && open[ref.Number] {
				g.dependents[ref.Number] = append(g.dependents[ref.Number], i.Number)
				g.blockers[i.Number] = append(g.blockers[i.Number], ref.Number)
			}
		}
	}
	return g
}

func (g Graph) Dependents(number int) []int {
	return g.dependents[number]
}

func (g Graph) Blockers(number int) []int {
	return g.blockers[number]
}

func (g Graph) downstream(number int) []int {
	seen := map[int]bool{number: true}
	var reached []int
	pending := []int{number}
	for len(pending) > 0 {
		current := pending[0]
		pending = pending[1:]
		for _, d := range g.dependents[current] {
			if !seen[d] {
				seen[d] = true
				reached = append(reached, d)
				pending = append(pending, d)
			}
		}
	}
	return reached
}
