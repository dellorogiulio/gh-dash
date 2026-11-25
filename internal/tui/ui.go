package tui

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	log "charm.land/log/v2"
	"github.com/atotto/clipboard"
	"github.com/cli/go-gh/v2/pkg/browser"
	"github.com/cli/go-gh/v2/pkg/repository"
	zone "github.com/lrstanley/bubblezone/v2"

	gitm "github.com/aymanbagabas/git-module"
	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/git"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/common"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branch"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/branchsidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/footer"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issuessection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/issueview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prrow"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prssection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/prview"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/queuesection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/reposection"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/section"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/sidebar"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tabs"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/components/tasks"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/constants"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/keys"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/markdown"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

type Model struct {
	keys             *keys.KeyMap
	sidebar          sidebar.Model
	prView           prview.Model
	issueSidebar     issueview.Model
	branchSidebar    branchsidebar.Model
	currSectionId    int
	footer           footer.Model
	repo             section.Section
	prs              []section.Section
	issues           []section.Section
	queue            []section.Section
	tabs             tabs.Model
	ctx              *context.ProgramContext
	taskSpinner      spinner.Model
	tasks            map[string]context.Task
	positionOverride string // "" means no override, "right" or "bottom"
	mode             Mode
}

type Mode int

const (
	ModeNone Mode = iota
	ModeSection
)

type Repositories struct {
	GHRepo  *repository.Repository
	GitRepo *gitm.Repository
}

func NewModel(location config.Location, repos Repositories) Model {
	taskSpinner := spinner.Model{Spinner: spinner.Dot}
	m := Model{
		keys:        keys.Keys,
		sidebar:     sidebar.NewModel(),
		taskSpinner: taskSpinner,
		tasks:       map[string]context.Task{},
	}

	version := "dev"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Sum != "" {
		version = info.Main.Version
	}

	m.ctx = &context.ProgramContext{
		GHRepo:     repos.GHRepo,
		GitRepo:    repos.GitRepo,
		ConfigFlag: location.ConfigFlag,
		RepoPath:   location.RepoPath,
		Version:    version,
		StartTask: func(task context.Task) tea.Cmd {
			log.Info("Starting task", "id", task.Id)
			task.StartTime = time.Now()
			m.tasks[task.Id] = task
			return m.taskSpinner.Tick
		},
		HasDarkBackground: true,
		BackgroundSource:  "default",
		Theme:             *theme.DefaultTheme,
		Styles:            context.DefaultStyles,
	}

	m.footer = footer.NewModel(m.ctx)
	m.prView = prview.NewModel(m.ctx)
	m.issueSidebar = issueview.NewModel(m.ctx)
	m.branchSidebar = branchsidebar.NewModel(m.ctx)
	m.tabs = tabs.NewModel(m.ctx)

	return m
}

func (m *Model) initScreen() tea.Msg {
	showError := func(err error) {
		styles := log.DefaultStyles()
		styles.Key = lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Bold(true)
		styles.Separator = lipgloss.NewStyle()

		logger := log.New(os.Stderr)
		logger.SetStyles(styles)
		logger.SetTimeFormat(time.RFC3339)
		logger.SetReportTimestamp(true)
		logger.SetPrefix("Reading config file")
		logger.SetReportCaller(true)

		logger.
			Fatal(
				"failed parsing config file",
				"location",
				m.ctx.ConfigFlag,
				"err",
				err,
			)
	}

	cfg, err := config.ParseConfig(
		config.Location{RepoPath: m.ctx.RepoPath, ConfigFlag: m.ctx.ConfigFlag},
	)
	if err != nil {
		showError(err)
		return initMsg{Config: cfg}
	}

	var url string
	if config.IsFeatureEnabled(config.FF_REPO_VIEW) && m.ctx.RepoPath != "" {
		res, err := git.GetOriginUrl(m.ctx.RepoPath)
		if err != nil {
			showError(err)
			return initMsg{Config: cfg}
		}
		url = res
	}

	err = keys.Rebind(
		cfg.Keybindings.Universal,
		cfg.Keybindings.Issues,
		cfg.Keybindings.Prs,
		cfg.Keybindings.Branches,
		cfg.Keybindings.Cmp,
	)
	if err != nil {
		showError(err)
	}

	return initMsg{Config: cfg, RepoUrl: url}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.initScreen)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd             tea.Cmd
		tabsCmd         tea.Cmd
		sidebarCmd      tea.Cmd
		prViewCmd       tea.Cmd
		issueSidebarCmd tea.Cmd
		footerCmd       tea.Cmd
		cmds            []tea.Cmd
		currSection     = m.getCurrSection()
		currRowData     = m.getCurrRowData()
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		log.Info("Key pressed", "key", msg.String())
		m.ctx.Error = nil

		if currSection != nil && (currSection.IsSearchFocused() ||
			currSection.IsPromptConfirmationFocused()) {
			cmd = m.updateSection(currSection.GetId(), currSection.GetType(), msg)
			return m, cmd
		}

		if m.prView.IsTextInputBoxFocused() {
			m.prView, cmd = m.prView.Update(msg)
			m.syncSidebar()
			return m, cmd
		}

		if m.issueSidebar.IsTextInputBoxFocused() {
			m.issueSidebar, cmd, _ = m.issueSidebar.Update(msg)
			m.syncSidebar()
			return m, cmd
		}

		if m.footer.ShowConfirmQuit && (msg.String() == "y" || msg.String() == "enter") {
			return m, tea.Quit
		} else if m.footer.ShowConfirmQuit {
			m.footer.SetShowConfirmQuit(false)
			return m, nil
		}

		switch {
		case m.isUserDefinedKeybinding(msg):
			cmd = m.executeKeybinding(msg.String())
			return m, cmd

		case m.mode == ModeSection:
			if key.Matches(msg, m.keys.NewSection) {
				m.addNewSection()
			}

			if key.Matches(msg, m.keys.RemoveSection) {
				m.removeSection()
			}

			// always exit section mode if any key is pressed after
			m.mode = ModeNone

		case key.Matches(msg, m.keys.SectionMode):
			m.mode = ModeSection

		case key.Matches(msg, m.keys.PrevSection):
			prevSection := m.getSectionAt(m.getPrevSectionId())
			if prevSection != nil {
				m.setCurrSectionId(prevSection.GetId())
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.NextSection):
			nextSectionId := m.getNextSectionId()
			nextSection := m.getSectionAt(nextSectionId)
			if nextSection != nil {
				m.setCurrSectionId(nextSection.GetId())
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.Down):
			if currSection != nil {
				prevRow := currSection.CurrRow()
				nextRow := currSection.NextRow()
				if prevRow != nextRow && nextRow == currSection.NumRows()-1 &&
					m.ctx.View != config.RepoView {
					cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
				}
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.Up):
			if currSection != nil {
				currSection.PrevRow()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.FirstLine):
			if currSection != nil {
				currSection.FirstItem()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.LastLine):
			if currSection != nil {
				if currSection.CurrRow()+1 < currSection.NumRows() {
					cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
				}
				currSection.LastItem()
				cmd = m.onViewedRowChanged()
			}

		case key.Matches(msg, m.keys.TogglePreview):
			m.sidebar.IsOpen = !m.sidebar.IsOpen
			m.syncMainContentDimensions()

		case key.Matches(msg, m.keys.TogglePreviewPosition):
			if m.sidebar.IsOpen {
				if m.ctx.PreviewPosition == "right" {
					m.positionOverride = "bottom"
				} else {
					m.positionOverride = "right"
				}
				m.syncMainContentDimensions()
				m.syncProgramContext()
				cmd := m.syncSidebar()
				cmds = append(cmds, cmd)
			}

		case key.Matches(msg, m.keys.Refresh):
			if currSection != nil {
				data.ClearEnrichmentCache()
				currSection.ResetFilters()
				currSection.ResetRows()
				m.syncSidebar()
				cmds = append(cmds, currSection.SetIsLoading(true))
				cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
			}

		case key.Matches(msg, m.keys.RefreshAll):
			data.ClearEnrichmentCache()
			fetchSectionsCmds := m.fetchAllViewSections()
			m.updateTabs()
			cmds = append(cmds, fetchSectionsCmds)

		case key.Matches(msg, m.keys.Redraw):
			// with bubbletea v2's declarative approach, if we just clear the screen then tea will redraw for us
			return m, tea.ClearScreen

		case key.Matches(msg, m.keys.Search):
			if currSection != nil {
				cmd = currSection.SetIsSearching(true)
				return m, cmd
			}

		case key.Matches(msg, m.keys.Help):
			m.footer.ShowAll = !m.footer.ShowAll
			m.syncMainContentDimensions()

		case key.Matches(msg, m.keys.CopyNumber):
			var cmd tea.Cmd
			if currRowData == nil || reflect.ValueOf(currRowData).IsNil() {
				cmd = m.notifyErr("Current selection isn't associated with a PR/Issue")
				return m, cmd
			}
			number := fmt.Sprint(currRowData.GetNumber())
			err := clipboard.WriteAll(number)
			if err != nil {
				cmd = m.notifyErr(fmt.Sprintf("Failed copying to clipboard %v", err))
			} else {
				cmd = m.notify(fmt.Sprintf("Copied %s to clipboard", number))
			}
			return m, cmd

		case key.Matches(msg, m.keys.CopyUrl):
			var cmd tea.Cmd
			if currRowData == nil || reflect.ValueOf(currRowData).IsNil() {
				cmd = m.notifyErr("Current selection isn't associated with a PR/Issue")
				return m, cmd
			}
			url := currRowData.GetUrl()
			err := clipboard.WriteAll(url)
			if err != nil {
				cmd = m.notifyErr(fmt.Sprintf("Failed copying to clipboard %v", err))
			} else {
				cmd = m.notify(fmt.Sprintf("Copied %s to clipboard", url))
			}
			return m, cmd

		case key.Matches(msg, m.keys.Quit):
			if !m.ctx.Config.ConfirmQuit {
				return m, tea.Quit
			}

			m.footer.SetShowConfirmQuit(true)

		case m.ctx.View == config.RepoView:
			switch {
			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.repo.(*reposection.Model).OpenGithub())

			case key.Matches(msg, keys.BranchKeys.Delete):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("delete")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.New):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("new")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.CreatePr):
				if currSection != nil {
					currSection.SetPromptConfirmationAction("create_pr")
					cmd = currSection.SetIsPromptConfirmationShown(true)
				}
				return m, cmd

			case key.Matches(msg, keys.BranchKeys.ViewPRs):
				cmds = append(cmds, m.switchSelectedView())
			}
		case m.ctx.View == config.PRsView:
			switch {
			case key.Matches(msg, keys.PRKeys.PrevSidebarTab),
				key.Matches(msg, keys.PRKeys.NextSidebarTab):
				var scmds []tea.Cmd
				var scmd tea.Cmd
				m.prView, scmd = m.prView.Update(msg)
				scmds = append(scmds, scmd)
				m.syncSidebar()
				return m, tea.Batch(scmds...)

			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.openBrowser())

			case key.Matches(msg, keys.PRKeys.Approve):
				return m, m.openSidebarForPRInput(m.prView.SetIsApproving)

			case key.Matches(msg, keys.PRKeys.Assign):
				return m, m.openSidebarForPRInput(m.prView.SetIsAssigning)

			case key.Matches(msg, keys.PRKeys.Unassign):
				return m, m.openSidebarForPRInput(m.prView.SetIsUnassigning)

			case key.Matches(msg, keys.PRKeys.Label):
				return m, m.openSidebarForPRInput(m.prView.SetIsLabeling)

			case key.Matches(msg, keys.PRKeys.Comment):
				return m, m.openSidebarForPRInput(m.prView.SetIsCommenting)

			case key.Matches(msg, keys.PRKeys.Close):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "close")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Ready):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "ready")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Reopen):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "reopen")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Merge):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "merge")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.Update):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "update")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.ApproveWorkflows):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "approveWorkflows")
				}
				return m, cmd

			case key.Matches(msg, keys.PRKeys.ViewIssues):
				cmds = append(cmds, m.switchSelectedView())

			case key.Matches(msg, keys.PRKeys.SummaryViewMore):
				m.prView.SetSummaryViewMore()
				m.syncSidebar()
				return m, nil
			}
		case m.ctx.View == config.IssuesView || m.ctx.View == config.QueueView:
			switch {
			case key.Matches(msg, m.keys.OpenGithub):
				cmds = append(cmds, m.openBrowser())

			case key.Matches(msg, keys.IssueKeys.Label):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsLabeling)

			case key.Matches(msg, keys.IssueKeys.Assign):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsAssigning)

			case key.Matches(msg, keys.IssueKeys.Unassign):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsUnassigning)

			case key.Matches(msg, keys.IssueKeys.Comment):
				return m, m.openSidebarForInput(m.issueSidebar.SetIsCommenting)

			case key.Matches(msg, keys.IssueKeys.Checkout):
				cmd, err := m.issueSidebar.Checkout()
				if err != nil {
					m.ctx.Error = err
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.Close):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "close")
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.Reopen):
				if currRowData != nil {
					cmd = m.promptConfirmation(currSection, "reopen")
				}
				return m, cmd

			case key.Matches(msg, keys.IssueKeys.ViewPRs):
				cmds = append(cmds, m.switchSelectedView())
			}
		}

	case initMsg:
		m.ctx.Config = &msg.Config
		m.ctx.RepoUrl = msg.RepoUrl
		m.ctx.Theme = theme.ParseTheme(m.ctx.Config)
		m.ctx.Styles = context.InitStyles(m.ctx.Theme)
		m.taskSpinner.Style = lipgloss.NewStyle().
			Background(m.ctx.Theme.SelectedBackground)

		m.ctx.View = m.ctx.Config.Defaults.View
		m.currSectionId = 0
		m.sidebar.IsOpen = msg.Config.Defaults.Preview.Open
		m.syncMainContentDimensions()

		m.initSections()
		fetchSectionsCmds := m.fetchAllViewSections()
		m.updateTabs()
		m.tabs.SetCurrSectionId(0)

		if m.ctx.BackgroundSource != "bubbletea" {
			log.Debugf("Setting markdownStyle in initMsg")
			m.ctx.HasDarkBackground = compat.HasDarkBackground
			m.ctx.BackgroundSource = "compat"
			log.Debugf(
				"HasDarkBackground: %t, BackgroundSource: %s",
				m.ctx.HasDarkBackground,
				m.ctx.BackgroundSource,
			)
			markdown.InitializeMarkdownStyle(m.ctx)
		}

		cmds = append(cmds, fetchSectionsCmds, m.tabs.Init(), fetchUser,
			m.doRefreshAtInterval(), m.doUpdateFooterAtInterval())

	case intervalRefresh:
		fetchSectionsCmds := m.fetchAllViewSections()
		m.updateTabs()
		cmds = append(cmds, fetchSectionsCmds, m.doRefreshAtInterval())

	case userFetchedMsg:
		m.ctx.User = msg.user

	case constants.TaskFinishedMsg:
		task, ok := m.tasks[msg.TaskId]
		if ok {
			log.Info("Task finished", "id", task.Id)
			if msg.Err != nil {
				log.Error("Task finished with error", "id", task.Id, "err", msg.Err)
				task.State = context.TaskError
				task.Error = msg.Err
			} else {
				task.State = context.TaskFinished
			}
			now := time.Now()
			task.FinishedTime = &now
			m.tasks[msg.TaskId] = task
			clear := tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return constants.ClearTaskMsg{TaskId: msg.TaskId}
			})
			cmds = append(cmds, clear)

			scmd := m.updateSection(msg.SectionId, msg.SectionType, msg.Msg)
			cmds = append(cmds, scmd)

			syncCmd := m.syncSidebar()
			cmds = append(cmds, syncCmd)
		}

	case prview.EnrichedPrMsg:
		if msg.Err == nil {
			m.prView.SetEnrichedPR(msg.Data)
			m.prs[msg.Id].(*prssection.Model).EnrichPR(msg.Data)
			syncCmd := m.syncSidebar()
			cmds = append(cmds, syncCmd)
		} else {
			log.Error("failed enriching pr", "err", msg.Err)
		}

	case spinner.TickMsg:
		if len(m.tasks) > 0 {
			taskSpinner, internalTickCmd := m.taskSpinner.Update(msg)
			m.taskSpinner = taskSpinner
			rTask := m.renderRunningTask()
			m.footer.SetRightSection(rTask)
			cmd = internalTickCmd
		}

	case constants.ClearTaskMsg:
		m.footer.SetRightSection("")
		delete(m.tasks, msg.TaskId)

	case section.SectionMsg:
		cmd = m.updateRelevantSection(msg)

		if msg.Id == m.currSectionId {
			cmds = append(cmds, m.onViewedRowChanged())
		}

	case execProcessFinishedMsg, tea.FocusMsg:
		if currSection != nil {
			cmds = append(cmds, currSection.FetchNextPageSectionRows()...)
		}

	case tea.MouseClickMsg:
		if msg.Button != tea.MouseLeft {
			return m, nil
		}
		if zone.Get("donate").InBounds(msg) {
			log.Info("Donate clicked", "msg", msg)
			openCmd := func() tea.Msg {
				// Discard the launcher's stdout/stderr so any noise (e.g.
				// GTK / GVFS warnings from xdg-open / gnome-open) does not
				// leak into the TUI's terminal and corrupt the display.
				// See #829, #584, #679.
				b := browser.New("", io.Discard, io.Discard)
				err := b.Browse("https://github.com/sponsors/dlvhdr")
				if err != nil {
					return constants.ErrMsg{Err: err}
				}
				return nil
			}
			cmds = append(cmds, openCmd)
		}

	case tea.WindowSizeMsg:
		m.onWindowSizeChanged(msg)

	case tea.BackgroundColorMsg:
		log.Debugf("Setting markdownStyle in BackgroundColorMsg")
		m.ctx.HasDarkBackground = msg.IsDark()
		m.ctx.BackgroundSource = "bubbletea"
		log.Debugf(
			"HasDarkBackground: %t, BackgroundSource: %s",
			m.ctx.HasDarkBackground,
			m.ctx.BackgroundSource,
		)
		markdown.InitializeMarkdownStyle(m.ctx)

	case updateFooterMsg:
		cmds = append(cmds, cmd, m.doUpdateFooterAtInterval())

	case constants.ErrMsg:
		m.ctx.Error = msg.Err
	}

	m.syncProgramContext()

	var bsCmd tea.Cmd
	m.branchSidebar, bsCmd = m.branchSidebar.Update(msg)
	cmds = append(cmds, bsCmd)

	m.sidebar, sidebarCmd = m.sidebar.Update(msg)

	if m.prView.IsTextInputBoxFocused() {
		m.prView, prViewCmd = m.prView.Update(msg)
		m.syncSidebar()
	}

	if m.issueSidebar.IsTextInputBoxFocused() {
		m.issueSidebar, issueSidebarCmd, _ = m.issueSidebar.Update(msg)
		m.syncSidebar()
	}

	if currSection != nil {
		if currSection.IsPromptConfirmationFocused() {
			m.footer.SetLeftSection(currSection.GetPromptConfirmation())
		}

		if !currSection.IsPromptConfirmationFocused() {
			m.footer.SetLeftSection(currSection.GetPagerContent())
		}
	}

	tm, tabsCmd := m.tabs.Update(msg)
	m.tabs = tm

	sectionCmd := m.updateCurrentSection(msg)
	cmds = append(
		cmds,
		cmd,
		tabsCmd,
		sidebarCmd,
		footerCmd,
		sectionCmd,
		prViewCmd,
		issueSidebarCmd,
	)

	return m, tea.Batch(cmds...)
}

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.ReportFocus = true
	v.MouseMode = tea.MouseModeCellMotion

	if m.ctx.Config == nil {
		v.Content = lipgloss.Place(
			m.ctx.ScreenWidth,
			m.ctx.ScreenHeight,
			lipgloss.Center,
			lipgloss.Center,
			"Reading config...",
		)
		return v
	}

	s := strings.Builder{}
	if m.ctx.View != config.RepoView {
		s.WriteString(m.tabs.View())
	}
	s.WriteString("\n")
	var content string
	currSection := m.getCurrSection()
	if currSection != nil {
		if m.ctx.PreviewPosition == "bottom" && m.sidebar.IsOpen {
			content = lipgloss.JoinVertical(
				lipgloss.Left,
				m.getCurrSection().View(),
				m.sidebar.View(),
			)
		} else {
			content = lipgloss.JoinHorizontal(
				lipgloss.Top,
				m.getCurrSection().View(),
				m.sidebar.View(),
			)
		}
	} else {
		content = lipgloss.Place(
			m.ctx.MainContentWidth,
			m.ctx.MainContentHeight,
			lipgloss.Center,
			lipgloss.Center,
			"No sections defined",
		)
	}
	s.WriteString(content)
	s.WriteString("\n")
	if m.ctx.Error != nil {
		s.WriteString(
			m.ctx.Styles.Common.ErrorStyle.
				Width(m.ctx.ScreenWidth).
				Render(fmt.Sprintf("%s %s",
					m.ctx.Styles.Common.FailureGlyph,
					lipgloss.NewStyle().
						Foreground(m.ctx.Theme.ErrorText).
						Render(m.ctx.Error.Error()),
				)),
		)
	} else {
		s.WriteString(m.footer.View())
	}

	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(zone.Scan(s.String())),
	}

	if currSection != nil {
		searchCmp := currSection.ViewCompletions()
		if searchCmp != "" {
			y := common.HeaderHeight + common.SearchHeight
			layers = append(layers, lipgloss.NewLayer(searchCmp).X(1).Y(y))
		}
	}

	prCmp := m.prView.ViewCompletions()
	previewPos := m.ctx.PreviewCursorPosition()
	if prCmp != "" {
		y := m.ctx.ScreenHeight - common.FooterHeight - m.prView.InputBoxLineFromBottom() - common.InputBoxHeight - 6
		layers = append(layers, lipgloss.NewLayer(prCmp).X(previewPos.X+3).Y(y))
	}

	issueCmp := m.issueSidebar.ViewCompletions()
	if issueCmp != "" {
		y := m.ctx.ScreenHeight - common.FooterHeight - m.issueSidebar.InputBoxLineFromButton() - common.InputBoxHeight - 6
		layers = append(layers, lipgloss.NewLayer(issueCmp).X(previewPos.X+3).Y(y))
	}

	comp := lipgloss.NewCompositor(layers...)
	v.SetContent(comp.Render())

	return v
}

type initMsg struct {
	Config  config.Config
	RepoUrl string
}

func (m *Model) setCurrSectionId(newSectionId int) {
	m.currSectionId = newSectionId
	m.tabs.SetCurrSectionId(newSectionId)
}

func (m *Model) onViewedRowChanged() tea.Cmd {
	m.prView.SetSummaryViewLess()
	m.prView.GoToFirstTab()
	sidebarCmd := m.syncSidebar()
	enrichCmd := m.prView.EnrichCurrRow()
	m.sidebar.ScrollToTop()
	return tea.Batch(sidebarCmd, enrichCmd)
}

func (m *Model) onWindowSizeChanged(msg tea.WindowSizeMsg) {
	log.Info("window size changed", "width", msg.Width, "height", msg.Height)
	m.footer.SetWidth(msg.Width)
	m.ctx.ScreenWidth = msg.Width
	m.ctx.ScreenHeight = msg.Height
	if m.ctx.Config != nil {
		if m.ctx.Config.Defaults.Preview.Position == "auto" ||
			m.ctx.Config.Defaults.Preview.Position == "" {
			m.positionOverride = ""
		}
		m.syncMainContentDimensions()
		m.syncSidebar()
	}
}

func (m *Model) syncProgramContext() {
	for _, section := range m.getCurrentViewSections() {
		section.UpdateProgramContext(m.ctx)
	}
	m.tabs.UpdateProgramContext(m.ctx)
	m.footer.UpdateProgramContext(m.ctx)
	m.sidebar.UpdateProgramContext(m.ctx)
	m.prView.UpdateProgramContext(m.ctx)
	m.issueSidebar.UpdateProgramContext(m.ctx)
	m.branchSidebar.UpdateProgramContext(m.ctx)
}

func (m *Model) updateSection(id int, sType string, msg tea.Msg) (cmd tea.Cmd) {
	var updatedSection section.Section
	switch sType {
	case reposection.SectionType:
		m.repo, cmd = m.repo.Update(msg)

	case prssection.SectionType:
		updatedSection, cmd = m.prs[id].Update(msg)
		m.prs[id] = updatedSection
	case issuessection.SectionType:
		updatedSection, cmd = m.issues[id].Update(msg)
		m.issues[id] = updatedSection
	case queuesection.SectionType:
		updatedSection, cmd = m.queue[id].Update(msg)
		m.queue[id] = updatedSection
	}

	currSection := m.getCurrSection()
	if currSection != nil && id == currSection.GetId() {
		if _, ok := msg.(prssection.SectionPullRequestsFetchedMsg); ok {
			cmd = m.onViewedRowChanged()
		}
	}

	return cmd
}

func (m *Model) updateRelevantSection(msg section.SectionMsg) (cmd tea.Cmd) {
	return m.updateSection(msg.Id, msg.Type, msg)
}

func (m *Model) updateCurrentSection(msg tea.Msg) (cmd tea.Cmd) {
	section := m.getCurrSection()
	if section == nil {
		return nil
	}
	return m.updateSection(section.GetId(), section.GetType(), msg)
}

const minTableWidthForRightPreview = 80

func (m *Model) resolvePreviewPosition() string {
	pos := m.ctx.Config.Defaults.Preview.Position
	if pos == "" {
		pos = "auto"
	}

	if m.positionOverride != "" {
		return m.positionOverride
	}

	if pos == "right" || pos == "bottom" {
		return pos
	}

	// auto: check if right mode would leave enough room for the main content
	w := m.ctx.Config.Defaults.Preview.Width
	if w > 0 && w < 1 {
		w *= float64(m.ctx.ScreenWidth)
	}
	previewWidth := min(int(w), m.ctx.ScreenWidth)
	tableWidth := m.ctx.ScreenWidth - previewWidth
	if tableWidth < minTableWidthForRightPreview {
		return "bottom"
	}
	return "right"
}

func (m *Model) getBaseContentHeight() int {
	if m.footer.ShowAll {
		// Measure actual footer height — the ExpandedHelpHeight constant
		// doesn't account for custom keybindings or view-specific bindings.
		footerHeight := lipgloss.Height(m.footer.View())
		return m.ctx.ScreenHeight - common.TabsHeight - footerHeight
	}
	return m.ctx.ScreenHeight - common.TabsHeight - common.FooterHeight
}

func (m *Model) syncMainContentDimensions() {
	m.ctx.PreviewPosition = m.resolvePreviewPosition()

	if !m.sidebar.IsOpen {
		m.ctx.MainContentWidth = m.ctx.ScreenWidth
		m.ctx.MainContentHeight = m.getBaseContentHeight()
		m.ctx.DynamicPreviewWidth = 0
		m.ctx.DynamicPreviewHeight = 0
		m.ctx.SidebarOpen = false
		return
	}

	m.ctx.SidebarOpen = true

	if m.ctx.PreviewPosition == "bottom" {
		m.ctx.MainContentWidth = m.ctx.ScreenWidth

		// Subtract border height: lipgloss Height() sets content height,
		// and BorderTop adds an extra row outside of that.
		availableHeight := m.getBaseContentHeight() - m.ctx.Styles.Sidebar.BorderWidth
		h := m.ctx.Config.Defaults.Preview.Height
		if h > 0 && h < 1 {
			h *= float64(availableHeight)
		}
		m.ctx.DynamicPreviewHeight = min(int(h), availableHeight)
		m.ctx.MainContentHeight = availableHeight - m.ctx.DynamicPreviewHeight
		m.ctx.DynamicPreviewWidth = m.ctx.ScreenWidth
	} else {
		m.ctx.MainContentHeight = m.getBaseContentHeight()

		w := m.ctx.Config.Defaults.Preview.Width
		if w > 0 && w < 1 {
			w *= float64(m.ctx.ScreenWidth)
		}
		m.ctx.DynamicPreviewWidth = min(int(w), m.ctx.ScreenWidth)
		m.ctx.MainContentWidth = m.ctx.ScreenWidth - m.ctx.DynamicPreviewWidth
		m.ctx.DynamicPreviewHeight = 0
	}
}

func (m *Model) openSidebarForPRInput(setFunc func(bool) tea.Cmd) tea.Cmd {
	m.prView.GoToFirstTab()
	return m.openSidebarForInput(setFunc)
}

func (m *Model) openSidebarForInput(setFunc func(bool) tea.Cmd) tea.Cmd {
	m.sidebar.IsOpen = true
	cmd := setFunc(true)
	m.syncMainContentDimensions()
	m.syncSidebar()
	m.sidebar.ScrollToBottom()
	return cmd
}

func (m *Model) promptConfirmation(currSection section.Section, action string) tea.Cmd {
	if currSection != nil {
		currSection.SetPromptConfirmationAction(action)
		return currSection.SetIsPromptConfirmationShown(true)
	}
	return nil
}

func (m *Model) syncSidebar() tea.Cmd {
	if !m.sidebar.IsOpen {
		return nil
	}

	currRowData := m.getCurrRowData()
	width := m.sidebar.GetSidebarContentWidth()
	var cmd tea.Cmd

	if currRowData == nil {
		m.sidebar.SetContent("")
		return nil
	}

	switch row := currRowData.(type) {
	case branch.BranchData:
		cmd = m.branchSidebar.SetRow(&row)
		m.sidebar.SetContent(m.branchSidebar.View())
	case *prrow.Data:
		m.prView.SetSectionId(m.currSectionId)
		m.prView.SetRow(row)
		m.prView.SetWidth(width)
		m.sidebar.SetContent(m.prView.View())
		// Scroll to bottom if in input mode to keep inputbox visible
		if m.prView.IsTextInputBoxFocused() {
			m.sidebar.ScrollToBottom()
		}
	case *data.IssueData:
		m.issueSidebar.SetSection(tasks.SectionIdentifier{
			Id:   m.currSectionId,
			Type: m.getCurrSection().GetType(),
		})
		m.issueSidebar.SetRow(row)
		if queueTab, ok := m.getCurrSection().(*queuesection.Model); ok {
			m.issueSidebar.SetDependencies(queueTab.DependencyTrees())
		}
		m.issueSidebar.SetWidth(width)
		m.sidebar.SetContent(m.issueSidebar.View())
		// Scroll to bottom if in input mode to keep inputbox visible
		if m.issueSidebar.IsTextInputBoxFocused() {
			m.sidebar.ScrollToBottom()
		}
	}

	return cmd
}

func (m *Model) fetchAllViewSections() tea.Cmd {
	cmds := make([]tea.Cmd, 0)
	cmds = append(cmds, m.tabs.SetAllLoading()...)

	switch m.ctx.View {
	case config.RepoView:
		var cmd tea.Cmd
		s, cmd := reposection.FetchAllBranches(m.ctx)
		cmds = append(cmds, cmd)
		m.repo = &s
		return tea.Batch(cmds...)
	case config.PRsView:
		prcmds := prssection.FetchAllSections(m.ctx, m.prs)
		cmds = append(cmds, prcmds)
		return tea.Batch(cmds...)
	case config.QueueView:
		cmds = append(cmds, queuesection.FetchAllSections(m.ctx, m.queue))
		return tea.Batch(cmds...)
	default:
		issuecmds := issuessection.FetchAllSections(m.ctx, m.issues)
		cmds = append(cmds, issuecmds)
		return tea.Batch(cmds...)
	}
}

func (m *Model) getCurrentViewSections() []section.Section {
	switch m.ctx.View {
	case config.RepoView:
		if m.repo == nil {
			return []section.Section{}
		}
		return []section.Section{m.repo}
	case config.PRsView:
		return m.prs
	case config.QueueView:
		return m.queue
	default:
		return m.issues
	}
}

func (m *Model) updateTabs() {
	sections := m.getCurrentViewSections()
	m.tabs.SetSections(sections)
}

func (m *Model) switchSelectedView() tea.Cmd {
	repoFF := config.IsFeatureEnabled(config.FF_REPO_VIEW)

	switch m.ctx.View {
	case config.PRsView:
		m.ctx.View = config.IssuesView
	case config.IssuesView:
		m.ctx.View = config.QueueView
	case config.QueueView:
		if repoFF {
			m.ctx.View = config.RepoView
		} else {
			m.ctx.View = config.PRsView
		}
	default:
		m.ctx.View = config.PRsView
	}

	m.syncMainContentDimensions()
	m.setCurrSectionId(0)

	var cmds []tea.Cmd
	if m.isViewSectionsStale() {
		cmds = append(cmds, m.fetchAllViewSections())
	}
	m.updateTabs()
	cmds = append(cmds, m.onViewedRowChanged())

	return tea.Batch(cmds...)
}

func (m *Model) isUserDefinedKeybinding(msg tea.KeyMsg) bool {
	if m.ctx == nil || m.ctx.Config == nil {
		return false
	}
	for _, keybinding := range m.ctx.Config.Keybindings.Universal {
		if keybinding.Builtin == "" && keybinding.Key == msg.String() {
			return true
		}
	}

	if m.ctx.View == config.IssuesView || m.ctx.View == config.QueueView {
		for _, keybinding := range m.ctx.Config.Keybindings.Issues {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	if m.ctx.View == config.PRsView {
		for _, keybinding := range m.ctx.Config.Keybindings.Prs {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	if m.ctx.View == config.RepoView {
		for _, keybinding := range m.ctx.Config.Keybindings.Branches {
			if keybinding.Builtin == "" && keybinding.Key == msg.String() {
				return true
			}
		}
	}

	return false
}

func (m *Model) renderRunningTask() string {
	tasks := make([]context.Task, 0, len(m.tasks))
	for _, value := range m.tasks {
		tasks = append(tasks, value)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].FinishedTime != nil && tasks[j].FinishedTime == nil {
			return false
		}
		if tasks[j].FinishedTime != nil && tasks[i].FinishedTime == nil {
			return true
		}
		if tasks[j].FinishedTime != nil && tasks[i].FinishedTime != nil {
			return tasks[i].FinishedTime.After(*tasks[j].FinishedTime)
		}

		return tasks[i].StartTime.After(tasks[j].StartTime)
	})
	task := tasks[0]

	var currTaskStatus string
	switch task.State {
	case context.TaskStart:
		currTaskStatus = lipgloss.JoinHorizontal(
			lipgloss.Top,
			m.taskSpinner.View(),
			lipgloss.NewStyle().
				Background(m.ctx.Theme.SelectedBackground).Render(task.StartText),
		)
	case context.TaskError:
		currTaskStatus = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.ErrorText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("%s %s", constants.FailureIcon, task.Error.Error()))
	case context.TaskFinished:
		currTaskStatus = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.SuccessText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("%s %s", constants.SuccessIcon, task.FinishedText))
	}

	var numProcessing int
	for _, task := range m.tasks {
		if task.State == context.TaskStart {
			numProcessing += 1
		}
	}

	stats := ""
	if numProcessing > 1 {
		stats = lipgloss.NewStyle().
			Foreground(m.ctx.Theme.FaintText).
			Background(m.ctx.Theme.SelectedBackground).
			Render(fmt.Sprintf("[ %d] ", numProcessing))
	}

	return lipgloss.NewStyle().
		Padding(0, 1).
		Height(1).
		Background(m.ctx.Theme.SelectedBackground).
		Render(strings.TrimSpace(lipgloss.JoinHorizontal(lipgloss.Top, stats, currTaskStatus)))
}

type userFetchedMsg struct {
	user string
}

func fetchUser() tea.Msg {
	user, err := data.CurrentLoginName()
	if err != nil {
		return constants.ErrMsg{
			Err: err,
		}
	}

	return userFetchedMsg{
		user: user,
	}
}

type intervalRefresh time.Time

func (m *Model) doRefreshAtInterval() tea.Cmd {
	if m.ctx.Config.Defaults.RefetchIntervalMinutes == 0 {
		return nil
	}

	return tea.Tick(
		time.Minute*time.Duration(m.ctx.Config.Defaults.RefetchIntervalMinutes),
		func(t time.Time) tea.Msg {
			return intervalRefresh(t)
		},
	)
}

type updateFooterMsg struct{}

func (m *Model) doUpdateFooterAtInterval() tea.Cmd {
	return tea.Tick(
		time.Second*10,
		func(t time.Time) tea.Msg {
			return updateFooterMsg{}
		},
	)
}

func (m *Model) addNewSection() {
	var sid int
	switch m.ctx.View {
	case config.IssuesView:
		sid = len(m.issues)
		m.issues = append(m.issues, new(issuessection.NewModel(
			sid,
			m.ctx,
			config.IssuesSectionConfig{
				Title:   "Scratch",
				Filters: "archived:false",
			},
			time.Now(),
			time.Now(),
		)))
	case config.PRsView:
		sid = len(m.prs)
		m.prs = append(m.prs, new(prssection.NewModel(
			sid,
			m.ctx,
			config.PrsSectionConfig{
				Title:   "Scratch",
				Filters: "archived:false",
			},
			time.Now(),
			time.Now(),
		)))
	}
	m.updateTabs()
	m.setCurrSectionId(sid)
}

func (m *Model) removeSection() {
	cnt := len(m.getCurrentViewSections())
	// don't allow removing the last section
	if cnt <= 1 {
		return
	}

	del := m.currSectionId
	switch m.ctx.View {
	case config.IssuesView:
		m.issues = slices.Delete(m.issues, del, del+1)
	case config.PRsView:
		m.prs = slices.Delete(m.prs, del, del+1)
	}

	for i, s := range m.getCurrentViewSections() {
		s.SetId(i)
	}

	nCnt := len(m.getCurrentViewSections())
	m.updateTabs()
	m.setCurrSectionId(min(nCnt-1, del))
}

func (m *Model) initSections() {
	m.prs = prssection.InitSections(m.ctx)
	m.issues = issuessection.InitSections(m.ctx)
	m.queue = queuesection.InitSections(m.ctx)
}

func (m *Model) isViewSectionsStale() bool {
	for _, section := range m.getCurrentViewSections() {
		if section.IsDataStale() {
			return true
		}
	}
	return false
}
