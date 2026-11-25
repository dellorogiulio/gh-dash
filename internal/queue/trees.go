package queue

import "slices"

type NodeStatus int

const (
	Outside NodeStatus = iota
	Listed
	Closed
)

// Node is an issue in a dependency tree. State applies when Status is Listed,
// that is when the issue is a row of the Queue or Needs me tab. A Cycle node
// repeats an ancestor and is not expanded.
type Node struct {
	Ref      Ref
	Title    string
	Status   NodeStatus
	State    State
	Cycle    bool
	Children []Node
}

func (a Analysis) UpstreamTree(number int) Node {
	return a.tree(Ref{Number: number}, nil, func(n int) []Ref {
		return Blockers(a.analyzer.open[n].Body, a.analyzer.launchOwner, a.analyzer.launchRepo)
	})
}

func (a Analysis) DownstreamTree(number int) Node {
	return a.tree(Ref{Number: number}, nil, func(n int) []Ref {
		var refs []Ref
		for _, d := range a.Graph.Dependents(n) {
			refs = append(refs, Ref{Number: d})
		}
		return refs
	})
}

func (a Analysis) tree(ref Ref, ancestors []int, children func(int) []Ref) Node {
	node := a.node(ref)
	if ref.Owner != "" || node.Status == Closed {
		return node
	}
	if slices.Contains(ancestors, ref.Number) {
		node.Cycle = true
		return node
	}
	ancestors = append(ancestors, ref.Number)
	for _, child := range children(ref.Number) {
		node.Children = append(node.Children, a.tree(child, ancestors, children))
	}
	return node
}

func (a Analysis) node(ref Ref) Node {
	node := Node{Ref: ref}
	if !a.analyzer.blocker(ref).Open {
		node.Status = Closed
		return node
	}
	if ref.Owner != "" {
		return node
	}
	node.Title = a.analyzer.open[ref.Number].Title
	if state, listed := a.states[ref.Number]; listed {
		node.Status, node.State = Listed, state
	}
	return node
}
