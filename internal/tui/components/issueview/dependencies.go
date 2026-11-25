package issueview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/queue"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
)

const (
	outsideIcon   = "⚪"
	closedIcon    = "⚫"
	cycleMark     = "↻"
	highlightMark = "▶"
)

const noHighlight = -1

// SetDependencies shows the trees of what blocks the issue and what it
// unblocks. SetRow clears them, so only the queue tabs set them.
func (m *Model) SetDependencies(upstream, downstream queue.Node) {
	m.upstream, m.downstream = upstream, downstream
}

// ResetHighlight is for when the selected row or the analysis changes: the
// trees are only redrawn while the preview is open, so they can't tell.
func (m *Model) ResetHighlight() {
	m.highlighted = noHighlight
}

// HighlightNextNode moves the highlight through the tree nodes in the order
// they are drawn, wrapping around.
func (m *Model) HighlightNextNode() {
	numNodes := len(m.treeNodes())
	if numNodes == 0 {
		return
	}
	m.highlighted = (m.highlighted + 1) % numNodes
}

func (m *Model) HighlightedNode() (queue.Node, bool) {
	nodes := m.treeNodes()
	if m.highlighted == noHighlight || m.highlighted >= len(nodes) {
		return queue.Node{}, false
	}
	return nodes[m.highlighted], true
}

type tree struct {
	title string
	root  queue.Node
}

func (m *Model) trees() [2]tree {
	return [2]tree{{"Blocked by", m.upstream}, {"Unblocks", m.downstream}}
}

type treeLine struct {
	prefix string
	node   queue.Node
}

func (m *Model) treeNodes() []queue.Node {
	var nodes []queue.Node
	for _, tree := range m.trees() {
		for _, line := range treeLines(tree.root, "") {
			nodes = append(nodes, line.node)
		}
	}
	return nodes
}

func (m *Model) renderDependencies() string {
	var sections []string
	index := 0
	for _, tree := range m.trees() {
		if len(tree.root.Children) == 0 {
			continue
		}
		lines := []string{m.renderTreeTitle(tree.title)}
		for _, line := range treeLines(tree.root, "") {
			lines = append(lines, line.prefix+m.renderNode(line.node, index == m.highlighted))
			index++
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return lipgloss.NewStyle().
		MaxWidth(m.getIndentedContentWidth()).
		Render(strings.Join(sections, "\n\n"))
}

func (m *Model) renderTreeTitle(title string) string {
	return m.ctx.Styles.Common.MainTextStyle.Underline(true).Render(title)
}

func treeLines(node queue.Node, indent string) []treeLine {
	var lines []treeLine
	for i, child := range node.Children {
		branch, childIndent := "├─ ", "│  "
		if i == len(node.Children)-1 {
			branch, childIndent = "└─ ", "   "
		}
		lines = append(lines, treeLine{indent + branch, child})
		lines = append(lines, treeLines(child, indent+childIndent)...)
	}
	return lines
}

func (m *Model) renderNode(node queue.Node, highlighted bool) string {
	parts := []string{nodeIcon(node), node.Ref.String()}
	switch {
	case node.Title != "":
		parts = append(parts, node.Title)
	case node.Status == queue.Closed:
		parts = append(parts, "closed")
	}
	if node.Cycle {
		parts = append(parts, cycleMark)
	}
	label := strings.Join(parts, " ")

	style := lipgloss.NewStyle()
	if node.Status == queue.Closed {
		style = style.Foreground(m.ctx.Theme.FaintText)
	}
	if highlighted {
		label = highlightMark + " " + label
		style = style.Bold(true).Background(m.ctx.Theme.SelectedBackground)
	}
	return style.Render(label)
}

func nodeIcon(node queue.Node) string {
	switch node.Status {
	case queue.Listed:
		return queuesection.StateIcon(node.State)
	case queue.Closed:
		return closedIcon
	default:
		return outsideIcon
	}
}
