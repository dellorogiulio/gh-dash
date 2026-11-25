package queuesection

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/queue"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuerow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/table"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
)

const SectionType = "queue"

// Model is one tab of the queue view. The tabs share one fetch of the open
// issues and render from one analysis of them.
type Model struct {
	section.BaseModel
	entriesOf func(queue.Analysis) []queue.Entry
	shared    *shared
}

type shared struct {
	tabs            []*Model
	openIssues      []data.IssueData
	otherRepoStates map[queue.Ref]string
	worktrees       map[int]bool
	analysis        queue.Analysis
	analysisChanges int
	analyzedFor     string
}

var (
	stateColumnWidth    = 24
	unblocksColumnWidth = 8
	worktreeColumnWidth = lipgloss.Width(worktreeMarker) + 2
)

const worktreeMarker = "⏵ worktree"

func newModel(
	id int,
	ctx *context.ProgramContext,
	cfg config.SectionConfig,
	shared *shared,
	entriesOf func(queue.Analysis) []queue.Entry,
) *Model {
	m := &Model{entriesOf: entriesOf, shared: shared}
	m.BaseModel = section.NewModel(
		ctx,
		section.NewSectionOptions{
			Id:     id,
			Config: cfg,
			Type:   SectionType,
			Columns: append(
				[]table.Column{
					{Title: "State", Width: &stateColumnWidth},
					{Title: "Unblocks", Width: &unblocksColumnWidth},
					{Title: "Worktree", Width: &worktreeColumnWidth},
				},
				issuessection.GetSectionColumns(config.IssuesSectionConfig{}, ctx)...,
			),
			Singular:    m.GetItemSingularForm(),
			Plural:      m.GetItemPluralForm(),
			LastUpdated: time.Now(),
			CreatedAt:   time.Now(),
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
		for i, currIssue := range m.shared.openIssues {
			if currIssue.Number == msg.IssueNumber {
				m.shared.openIssues[i] = msg.Apply(currIssue)
				m.shared.analyze(m.Ctx)
				m.SetIsLoading(false)
				break
			}
		}

	case SectionQueueFetchedMsg:
		if m.LastFetch.TaskId == msg.TaskId {
			m.shared.openIssues = msg.Issues
			m.shared.otherRepoStates = msg.OtherRepoStates
			m.shared.worktrees = msg.Worktrees
			m.shared.analyze(m.Ctx)
			for _, tab := range m.shared.tabs {
				tab.PageInfo = &data.PageInfo{}
				tab.SetIsLoading(false)
				tab.UpdateLastUpdated(time.Now())
			}
		}
	}

	search, searchCmd := m.SearchBar.Update(msg)
	m.SearchBar = search

	prompt, promptCmd := m.PromptConfirmationBox.Update(msg)
	m.PromptConfirmationBox = prompt

	table, tableCmd := m.Table.Update(msg)
	m.Table = table
	for _, tab := range m.shared.tabs {
		tab.Table.SetRows(tab.BuildRows())
		tab.UpdateTotalItemsCount(tab.GetTotalCount())
	}

	return m, tea.Batch(cmd, searchCmd, promptCmd, tableCmd)
}

func (s *shared) analyze(ctx *context.ProgramContext) {
	owner, name := "", ""
	if ctx.HasGHRepo() {
		owner, name = ctx.GHRepo.Owner, ctx.GHRepo.Name
	}
	analysis := queue.Analyze(s.openIssues, ctx.User, s.otherRepoStates, owner, name)
	if !reflect.DeepEqual(analysis, s.analysis) {
		s.analysisChanges++
	}
	s.analysis = analysis
	s.analyzedFor = ctx.User
}

// AnalysisChanges counts the times the analysis shared by the tabs changed.
func (m *Model) AnalysisChanges() int {
	return m.shared.analysisChanges
}

// The login is fetched in parallel with the issues, so the analysis is redone
// once it arrives.
func (m *Model) entries() []queue.Entry {
	if m.shared.analyzedFor != m.Ctx.User {
		m.shared.analyze(m.Ctx)
	}
	return m.entriesOf(m.shared.analysis)
}

func (m *Model) BuildRows() []table.Row {
	rows := []table.Row{}
	currItem := m.Table.GetCurrItem()
	for i, entry := range m.entries() {
		issueModel := issuerow.Issue{
			Ctx:            m.Ctx,
			Data:           entry.Issue,
			ShowAuthorIcon: m.ShowAuthorIcon,
			Columns:        m.Table.Columns[3:],
		}
		worktree := ""
		if m.shared.worktrees[entry.Issue.Number] {
			worktree = worktreeMarker
		}
		style := components.GetIssueTextStyle(m.Ctx)
		row := table.Row{
			style.Render(stateText(entry)),
			style.Render(strconv.Itoa(entry.Unblocks)),
			style.Render(worktree),
		}
		rows = append(rows, append(row, issueModel.ToTableRow(currItem == i)...))
	}
	return rows
}

func StateIcon(state queue.State) string {
	switch state {
	case queue.Next, queue.Ready:
		return "🟢"
	case queue.Parked:
		return "🟠"
	case queue.HITL:
		return "🟣"
	default:
		return "🔴"
	}
}

func stateText(entry queue.Entry) string {
	icon := StateIcon(entry.State)
	switch entry.State {
	case queue.Next:
		return icon + " Next"
	case queue.Ready:
		return icon + " Ready"
	case queue.Parked:
		return icon + " Parked"
	case queue.HITL:
		return icon + " HITL"
	default:
		blockers := []string{}
		for _, b := range entry.Blockers {
			if b.Open {
				blockers = append(blockers, blockerText(b))
			}
		}
		return icon + " Blocked by " + strings.Join(blockers, ", ")
	}
}

func blockerText(b queue.Blocker) string {
	switch {
	case b.HITL:
		return b.String() + " (HITL)"
	case b.Parked:
		return b.String() + " (parked)"
	default:
		return b.String()
	}
}

func (m *Model) NumRows() int {
	return len(m.entries())
}

func (m *Model) GetCurrRow() data.RowData {
	entries := m.entries()
	idx := m.Table.GetCurrItem()
	if idx < 0 || idx >= len(entries) {
		return nil
	}
	issue := entries[idx].Issue
	return &issue
}

// SelectIssue moves the cursor to the row of the issue, reporting whether the
// tab lists it.
func (m *Model) SelectIssue(number int) bool {
	row := slices.IndexFunc(m.entries(), func(e queue.Entry) bool {
		return e.Issue.Number == number
	})
	if row < 0 {
		return false
	}
	// NextItem stops at the table's last row, which must exist for the loop to end.
	m.Table.SetRows(m.BuildRows())
	for m.Table.GetCurrItem() < row {
		m.Table.NextItem()
	}
	for m.Table.GetCurrItem() > row {
		m.Table.PrevItem()
	}
	m.Table.SetRows(m.BuildRows())
	return true
}

func (m *Model) DependencyTrees() (upstream, downstream queue.Node) {
	issue, ok := m.GetCurrRow().(*data.IssueData)
	if !ok {
		return queue.Node{}, queue.Node{}
	}
	return m.shared.analysis.UpstreamTree(issue.Number),
		m.shared.analysis.DownstreamTree(issue.Number)
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
	repoPath, hasRepoPath := common.ResolveRepoPath(
		owner+"/"+name,
		m.Ctx.Config.RepoPaths,
		m.Ctx.RepoPath,
	)
	taskId := fmt.Sprintf("fetching_queue_%s", time.Now().String())
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

		otherRepoStates, err := data.FetchIssueStates(queue.OtherRepoBlockers(issues, owner, name))
		if err != nil {
			log.Warn("Failed to look up blockers in other repos", "err", err)
		}

		var worktrees map[int]bool
		if hasRepoPath {
			worktrees = wiqWorktrees(repoPath)
		}

		return constants.TaskFinishedMsg{
			SectionId:   m.Id,
			SectionType: m.Type,
			TaskId:      taskId,
			Msg: SectionQueueFetchedMsg{
				Issues:          issues,
				OtherRepoStates: otherRepoStates,
				Worktrees:       worktrees,
				TaskId:          taskId,
			},
		}
	}

	cmds := []tea.Cmd{startCmd, fetchCmd}
	for _, tab := range m.shared.tabs {
		tab.LastFetch.TaskId = taskId
		tab.LastFetch.At = time.Now()
		cmds = append(cmds, tab.SetIsLoading(true))
	}
	return cmds
}

func (m *Model) UpdateLastUpdated(t time.Time) {
	m.Table.SetLastUpdated(t)
}

func (m *Model) ResetRows() {
	m.shared.openIssues = nil
	m.shared.analyze(m.Ctx)
	for _, tab := range m.shared.tabs {
		tab.BaseModel.ResetRows()
	}
}

func InitSections(ctx *context.ProgramContext) []section.Section {
	s := &shared{}
	s.tabs = []*Model{
		newModel(0, ctx, config.QueueSectionConfig(), s, func(a queue.Analysis) []queue.Entry {
			return a.Queue
		}),
		newModel(1, ctx, config.NeedsMeSectionConfig(), s, func(a queue.Analysis) []queue.Entry {
			return a.NeedsMe
		}),
	}
	return []section.Section{s.tabs[0], s.tabs[1]}
}

func FetchAllSections(ctx *context.ProgramContext, existing []section.Section) tea.Cmd {
	tabs := section.ToImplSections[*Model](existing)
	if len(tabs) == 0 {
		return nil
	}
	for _, tab := range tabs {
		tab.SetLastUpdated(time.Now())
		tab.SetCreatedAt(time.Now())
		tab.PageInfo = nil
	}
	return tea.Batch(tabs[0].FetchNextPageSectionRows()...)
}

type SectionQueueFetchedMsg struct {
	Issues          []data.IssueData
	OtherRepoStates map[queue.Ref]string
	Worktrees       map[int]bool
	TaskId          string
}

// wiqWorktrees lists the issues that have a worktree from wiq, which creates
// it next to the checkout as <checkout folder>.wiq-issue-<number>.
func wiqWorktrees(repoPath string) map[int]bool {
	if underHome, ok := strings.CutPrefix(repoPath, "~"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		repoPath = home + underHome
	}
	repoPath = filepath.Clean(repoPath)
	prefix := filepath.Base(repoPath) + ".wiq-issue-"
	entries, err := os.ReadDir(filepath.Dir(repoPath))
	if err != nil {
		return nil
	}
	worktrees := map[int]bool{}
	for _, entry := range entries {
		suffix, isWiq := strings.CutPrefix(entry.Name(), prefix)
		number, err := strconv.Atoi(suffix)
		if isWiq && err == nil && entry.IsDir() {
			worktrees[number] = true
		}
	}
	return worktrees
}

func (m Model) GetItemSingularForm() string {
	return "Issue"
}

func (m Model) GetItemPluralForm() string {
	return "Issues"
}

func (m *Model) GetTotalCount() int {
	return len(m.entries())
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
