package issueview

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/queue"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
)

const (
	outsideIcon = "⚪"
	closedIcon  = "⚫"
	cycleMark   = "↻"
)

// SetDependencies shows the trees of what blocks the issue and what it
// unblocks. SetRow clears them, so only the queue tabs set them.
func (m *Model) SetDependencies(upstream, downstream queue.Node) {
	m.upstream, m.downstream = upstream, downstream
}

func (m *Model) renderDependencies() string {
	trees := []struct {
		title string
		root  queue.Node
	}{
		{"Blocked by", m.upstream},
		{"Unblocks", m.downstream},
	}

	var sections []string
	for _, tree := range trees {
		if len(tree.root.Children) == 0 {
			continue
		}
		lines := append([]string{m.renderTreeTitle(tree.title)}, m.treeLines(tree.root, "")...)
		sections = append(sections, strings.Join(lines, "\n"))
	}
	return lipgloss.NewStyle().
		MaxWidth(m.getIndentedContentWidth()).
		Render(strings.Join(sections, "\n\n"))
}

func (m *Model) renderTreeTitle(title string) string {
	return m.ctx.Styles.Common.MainTextStyle.Underline(true).Render(title)
}

func (m *Model) treeLines(node queue.Node, indent string) []string {
	var lines []string
	for i, child := range node.Children {
		branch, childIndent := "├─ ", "│  "
		if i == len(node.Children)-1 {
			branch, childIndent = "└─ ", "   "
		}
		lines = append(lines, indent+branch+m.renderNode(child))
		lines = append(lines, m.treeLines(child, indent+childIndent)...)
	}
	return lines
}

func (m *Model) renderNode(node queue.Node) string {
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

	if node.Status == queue.Closed {
		return lipgloss.NewStyle().Foreground(m.ctx.Theme.FaintText).Render(label)
	}
	return label
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
