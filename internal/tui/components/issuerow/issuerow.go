package issuerow

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
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

	width := issue.titleWidth() - 1
	firstLine := lipgloss.JoinHorizontal(lipgloss.Top, number, title)
	if width > 0 {
		firstLine = ansi.Truncate(firstLine, width, constants.Ellipsis)
	}

	labels := issue.renderLabels(isSelected, width)
	if labels == "" {
		return firstLine
	}
	return lipgloss.JoinVertical(lipgloss.Left, firstLine, labels)
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
	if len(issue.Data.Labels.Nodes) == 0 || width <= 2 {
		return ""
	}

	pillStyle := issue.Ctx.Styles.PrView.PillStyle
	rowStyle := lipgloss.NewStyle()
	if isSelected {
		rowStyle = rowStyle.Background(issue.Ctx.Theme.SelectedBackground)
		pillStyle = pillStyle.
			BorderLeftBackground(issue.Ctx.Theme.SelectedBackground).
			BorderRightBackground(issue.Ctx.Theme.SelectedBackground)
	}

	return common.RenderLabels(
		issue.Data.Labels.Nodes,
		common.LabelOpts{
			Width:     width,
			MaxRows:   1,
			PillStyle: pillStyle,
			RowStyle:  rowStyle,
		},
	)
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
