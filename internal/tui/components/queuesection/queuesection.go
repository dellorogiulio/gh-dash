package queuesection

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/queue"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuerow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

const SectionType = "queue"

type Model struct {
	section.BaseModel
	openIssues  []data.IssueData
	candidates  []data.IssueData
	filteredFor string
}

func NewModel(
	id int,
	ctx *context.ProgramContext,
	lastUpdated time.Time,
	createdAt time.Time,
) Model {
	m := Model{}
	m.BaseModel = section.NewModel(
		ctx,
		section.NewSectionOptions{
			Id:          id,
			Config:      config.QueueSectionConfig(),
			Type:        SectionType,
			Columns:     issuessection.GetSectionColumns(config.IssuesSectionConfig{}, ctx),
			Singular:    m.GetItemSingularForm(),
			Plural:      m.GetItemPluralForm(),
			LastUpdated: lastUpdated,
			CreatedAt:   createdAt,
		},
	)
	return m
}

func (m *Model) Update(msg tea.Msg) (section.Section, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.IsSearchFocused() {
			switch msg.String() {
			case "ctrl+c", "esc", "enter":
				m.SearchBar.SetValue(m.SearchValue)
				return m, m.SetIsSearching(false)
			}
			break
		}

		if m.IsPromptConfirmationFocused() {
			switch msg.String() {
			case "ctrl+c", "esc":
				m.PromptConfirmationBox.Reset()
				return m, m.SetIsPromptConfirmationShown(false)

			case "enter":
				input := m.PromptConfirmationBox.Value()
				if input == "Y" || input == "y" {
					issue := m.GetCurrRow()
					sid := tasks.SectionIdentifier{Id: m.Id, Type: SectionType}
					switch m.GetPromptConfirmationAction() {
					case "close":
						cmd = tasks.CloseIssue(m.Ctx, sid, issue)
					case "reopen":
						cmd = tasks.ReopenIssue(m.Ctx, sid, issue)
					}
				}

				m.PromptConfirmationBox.Reset()
				blinkCmd := m.SetIsPromptConfirmationShown(false)
				return m, tea.Batch(cmd, blinkCmd)
			}
			break
		}

	case tasks.UpdateIssueMsg:
		for i, currIssue := range m.openIssues {
			if currIssue.Number == msg.IssueNumber {
				m.openIssues[i] = msg.Apply(currIssue)
				m.refilter()
				m.SetIsLoading(false)
				break
			}
		}

	case SectionQueueFetchedMsg:
		if m.LastFetch.TaskId == msg.TaskId {
			m.openIssues = msg.Issues
			m.refilter()
			m.PageInfo = &data.PageInfo{}
			m.SetIsLoading(false)
			m.UpdateLastUpdated(time.Now())
		}
	}

	search, searchCmd := m.SearchBar.Update(msg)
	m.SearchBar = search

	prompt, promptCmd := m.PromptConfirmationBox.Update(msg)
	m.PromptConfirmationBox = prompt

	table, tableCmd := m.Table.Update(msg)
	m.Table = table
	m.Table.SetRows(m.BuildRows())
	m.UpdateTotalItemsCount(m.GetTotalCount())

	return m, tea.Batch(cmd, searchCmd, promptCmd, tableCmd)
}

func (m *Model) refilter() {
	m.candidates = queue.Candidates(m.openIssues, m.Ctx.User)
	m.filteredFor = m.Ctx.User
}

// The login is fetched in parallel with the issues, so candidates are
// recomputed once it arrives.
func (m *Model) issues() []data.IssueData {
	if m.filteredFor != m.Ctx.User {
		m.refilter()
	}
	return m.candidates
}

func (m *Model) BuildRows() []table.Row {
	rows := []table.Row{}
	currItem := m.Table.GetCurrItem()
	for i, currIssue := range m.issues() {
		issueModel := issuerow.Issue{
			Ctx:            m.Ctx,
			Data:           currIssue,
			ShowAuthorIcon: m.ShowAuthorIcon,
			Columns:        m.Table.Columns,
		}
		rows = append(rows, issueModel.ToTableRow(currItem == i))
	}
	return rows
}

func (m *Model) NumRows() int {
	return len(m.issues())
}

func (m *Model) GetCurrRow() data.RowData {
	issues := m.issues()
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(issues) {
		return nil
	}
	issue := issues[idx]
	return &issue
}

func (m *Model) FetchNextPageSectionRows() []tea.Cmd {
	if m == nil {
		return nil
	}

	if !m.Ctx.HasGHRepo() {
		m.SetIsLoading(false)
		return nil
	}

	if m.PageInfo != nil {
		return nil
	}

	owner, name := m.Ctx.GHRepo.Owner, m.Ctx.GHRepo.Name
	taskId := fmt.Sprintf("fetching_queue_%d_%s", m.Id, time.Now().String())
	m.LastFetch.TaskId = taskId
	m.LastFetch.At = time.Now()
	startCmd := m.Ctx.StartTask(context.Task{
		Id:           taskId,
		StartText:    fmt.Sprintf("Fetching open issues of %s/%s", owner, name),
		FinishedText: fmt.Sprintf("Open issues of %s/%s have been fetched", owner, name),
		State:        context.TaskStart,
	})

	fetchCmd := func() tea.Msg {
		issues, err := data.FetchAllOpenIssues(owner, name)
		if err != nil {
			return constants.TaskFinishedMsg{
				SectionId:   m.Id,
				SectionType: m.Type,
				TaskId:      taskId,
				Err:         err,
			}
		}

		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: m.Type,
			TaskId:      taskId,
			Msg:         SectionQueueFetchedMsg{Issues: issues, TaskId: taskId},
		}
	}

	return []tea.Cmd{startCmd, fetchCmd, m.SetIsLoading(true)}
}

func (m *Model) UpdateLastUpdated(t time.Time) {
	m.Table.SetLastUpdated(t)
}

func (m *Model) ResetRows() {
	m.openIssues = nil
	m.refilter()
	m.BaseModel.ResetRows()
}

func InitSections(ctx *context.ProgramContext) []section.Section {
	return []section.Section{new(NewModel(0, ctx, time.Now(), time.Now()))}
}

func FetchAllSections(ctx *context.ProgramContext, existing []section.Section) tea.Cmd {
	fetchCmds := make([]tea.Cmd, 0)
	for _, queueSection := range section.ToImplSections[*Model](existing) {
		queueSection.SetLastUpdated(time.Now())
		queueSection.SetCreatedAt(time.Now())
		queueSection.PageInfo = nil
		fetchCmds = append(fetchCmds, queueSection.FetchNextPageSectionRows()...)
	}
	return tea.Batch(fetchCmds...)
}

type SectionQueueFetchedMsg struct {
	Issues []data.IssueData
	TaskId string
}

func (m Model) GetItemSingularForm() string {
	return "Issue"
}

func (m Model) GetItemPluralForm() string {
	return "Issues"
}

func (m *Model) GetTotalCount() int {
	return len(m.issues())
}

func (m *Model) GetPagerContent() string {
	pagerContent := ""
	if total := m.GetTotalCount(); total > 0 {
		pagerContent = fmt.Sprintf(
			"%v %v • %v %v/%v",
			constants.WaitingIcon,
			m.LastUpdated().Format("01/02 15:04:05"),
			m.SingularForm,
			m.Table.GetCurrItem()+1,
			total,
		)
	}
	return m.Ctx.Styles.ListViewPort.PagerStyle.Render(pagerContent)
}
