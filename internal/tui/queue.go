package tui

import (
	"fmt"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
)

// The trees are only kept in sync with the selected row while the preview is open.
func (m *Model) isPreviewingQueue() bool {
	return m.ctx.View == config.QueueView && m.sidebar.IsOpen
}

func (m *Model) jumpToHighlightedDependency() tea.Cmd {
	node, ok := m.issueSidebar.HighlightedNode()
	if !ok {
		return nil
	}
	if node.Ref.Owner == "" {
		currentTabFirst := slices.Concat(m.queue[m.currSectionId:], m.queue[:m.currSectionId])
		for _, s := range currentTabFirst {
			if s.(*queuesection.Model).SelectIssue(node.Ref.Number) {
				m.setCurrSectionId(s.GetId())
				return m.onViewedRowChanged()
			}
		}
	}
	return m.openUrl(m.issueUrl(node.Ref))
}

func (m *Model) issueUrl(ref data.IssueRef) string {
	owner, repo := ref.Owner, ref.Repo
	if owner == "" {
		owner, repo = m.ctx.GHRepo.Owner, m.ctx.GHRepo.Name
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, ref.Number)
}
