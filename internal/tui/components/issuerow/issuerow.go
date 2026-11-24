package issuerow

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/utils"
)

type Issue struct {
	Ctx            *context.ProgramContext
	Data           data.IssueData
	ShowAuthorIcon bool
	Columns        []table.Column
}

func (issue *Issue) ToTableRow(isSelected bool) table.Row {
	return table.Row{
		issue.renderStatus(),
		issue.renderRepoName(),
		issue.renderTitle(isSelected),
		issue.renderOpenedBy(),
		issue.renderAssignees(),
		issue.renderNumComments(),
		issue.renderNumReactions(),
		issue.renderUpdateAt(),
		issue.renderCreatedAt(),
	}
}

func (issue *Issue) getTextStyle() lipgloss.Style {
	return components.GetIssueTextStyle(issue.Ctx)
}

func (issue *Issue) renderUpdateAt() string {
	timeFormat := issue.Ctx.Config.Defaults.DateFormat

	updatedAtOutput := ""
	if timeFormat == "" || timeFormat == "relative" {
		updatedAtOutput = utils.TimeElapsed(issue.Data.UpdatedAt)
	} else {
		updatedAtOutput = issue.Data.UpdatedAt.Format(timeFormat)
	}

	return issue.getTextStyle().Render(updatedAtOutput)
}

func (issue *Issue) renderCreatedAt() string {
	timeFormat := issue.Ctx.Config.Defaults.DateFormat

	createdAtOutput := ""
	if timeFormat == "" || timeFormat == "relative" {
		createdAtOutput = utils.TimeElapsed(issue.Data.CreatedAt)
	} else {
		createdAtOutput = issue.Data.CreatedAt.Format(timeFormat)
	}

	return issue.getTextStyle().Render(createdAtOutput)
}

func (issue *Issue) renderRepoName() string {
	repoName := issue.Data.Repository.Name
	return issue.getTextStyle().Render(repoName)
}

func (issue *Issue) renderTitle(isSelected bool) string {
	if issue.Ctx.Config.Theme.Ui.Table.Compact {
		return components.RenderIssueTitle(
			issue.Ctx,
			issue.Data.State,
			issue.Data.Title,
			issue.Data.Number,
		)
	}
	return issue.renderExtendedTitle(isSelected)
}

func (issue *Issue) renderExtendedTitle(isSelected bool) string {
	baseStyle := lipgloss.NewStyle()
	if isSelected {
		baseStyle = baseStyle.Background(issue.Ctx.Theme.SelectedBackground)
	}

	numberFg := issue.Ctx.Theme.SecondaryText
	if issue.Data.State != "OPEN" {
		numberFg = issue.Ctx.Theme.FaintText
	}
	number := baseStyle.Foreground(numberFg).Render(fmt.Sprintf("#%d ", issue.Data.Number))
	title := issue.getTextStyle().Inherit(baseStyle).Bold(true).Render(issue.Data.Title)

	width := issue.titleWidth() - 2
	subIssues := issue.renderSubIssuesProgress(baseStyle)
	firstLine := lipgloss.JoinHorizontal(lipgloss.Top, number, title)
	if width > 0 {
		firstLine = ansi.Truncate(firstLine, width-lipgloss.Width(subIssues), constants.Ellipsis)
	}
	firstLine += subIssues
	// Need to remove the last reset to prevent the selected row background from stopping.
	firstLine = utils.RemoveLastReset(firstLine)

	labels := issue.renderLabels(isSelected, width)
	if labels == "" {
		return firstLine
	}
	return lipgloss.JoinVertical(lipgloss.Left, firstLine, labels)
}

func (issue *Issue) renderSubIssuesProgress(baseStyle lipgloss.Style) string {
	summary := issue.Data.SubIssuesSummary
	if summary.Total == 0 {
		return ""
	}

	icon := baseStyle.Foreground(lipgloss.Color("#a371f7")).Render(progressIcon(summary.Completed, summary.Total))
	count := baseStyle.Foreground(issue.Ctx.Theme.SecondaryText).
		Render(fmt.Sprintf(" %d/%d", summary.Completed, summary.Total))
	return baseStyle.Render("  ") + icon + count
}

func progressIcon(completed, total int) string {
	switch ratio := float64(completed) / float64(total); {
	case ratio >= 1:
		return "●"
	case ratio >= 0.625:
		return "◕"
	case ratio >= 0.375:
		return "◑"
	case ratio > 0:
		return "◔"
	default:
		return "○"
	}
}

func (issue *Issue) titleWidth() int {
	for _, column := range issue.Columns {
		if column.Grow != nil && *column.Grow {
			return column.ComputedWidth
		}
	}
	return 0
}

func (issue *Issue) renderLabels(isSelected bool, width int) string {
	labels := issue.Data.Labels.Nodes
	if len(labels) == 0 || width <= 2 {
		return ""
	}

	baseStyle := lipgloss.NewStyle()
	if isSelected {
		baseStyle = baseStyle.Background(issue.Ctx.Theme.SelectedBackground)
	}
	nameStyle := baseStyle.Foreground(issue.Ctx.Theme.FaintText)
	separator := baseStyle.Render("  ")

	rendered := make([]string, 0, len(labels))
	usedWidth := 0
	for i, label := range labels {
		dot := baseStyle.Foreground(lipgloss.Color("#" + label.Color)).Render("●")
		item := dot + nameStyle.Render(" "+label.Name)
		itemWidth := lipgloss.Width(item)
		if usedWidth > 0 {
			itemWidth += lipgloss.Width(separator)
		}

		remaining := len(labels) - i - 1
		overflowWidth := 0
		if remaining > 0 {
			overflowWidth = lipgloss.Width(separator) + lipgloss.Width(fmt.Sprintf("+%d", remaining))
		}
		if usedWidth+itemWidth+overflowWidth > width && usedWidth > 0 {
			rendered = append(rendered, nameStyle.Render(fmt.Sprintf("+%d", len(labels)-i)))
			break
		}

		rendered = append(rendered, item)
		usedWidth += itemWidth
	}

	return ansi.Truncate(strings.Join(rendered, separator), width, constants.Ellipsis)
}

func (issue *Issue) renderOpenedBy() string {
	return issue.getTextStyle().Render(issue.Data.GetAuthor(issue.Ctx.Theme, issue.ShowAuthorIcon))
}

func (issue *Issue) renderAssignees() string {
	assignees := make([]string, 0, len(issue.Data.Assignees.Nodes))
	for _, assignee := range issue.Data.Assignees.Nodes {
		assignees = append(assignees, assignee.Login)
	}
	return issue.getTextStyle().Render(strings.Join(assignees, ","))
}

func (issue *Issue) renderStatus() string {
	if issue.Data.State == "OPEN" {
		return lipgloss.NewStyle().Foreground(issue.Ctx.Styles.Colors.OpenIssue).Render("")
	} else {
		return issue.getTextStyle().Render("")
	}
}

func (issue *Issue) renderNumComments() string {
	return issue.getTextStyle().Render(fmt.Sprintf("%d", issue.Data.Comments.TotalCount))
}

func (issue *Issue) renderNumReactions() string {
	return issue.getTextStyle().Render(fmt.Sprintf("%d", issue.Data.Reactions.TotalCount))
}
