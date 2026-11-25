package issueview

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"

	"github.com/dlvhdr/gh-dash/v4/internal/config"
	"github.com/dlvhdr/gh-dash/v4/internal/data"
	"github.com/dlvhdr/gh-dash/v4/internal/queue"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/context"
	"github.com/dlvhdr/gh-dash/v4/internal/tui/theme"
)

func newTestContext(t *testing.T) *context.ProgramContext {
	t.Helper()

	cfg, err := config.ParseConfig(config.Location{
		ConfigFlag:       "../../../config/testdata/test-config.yml",
		SkipGlobalConfig: true,
	})
	require.NoError(t, err)

	thm := theme.ParseTheme(&cfg)
	return &context.ProgramContext{
		Config:            &cfg,
		Theme:             thm,
		Styles:            context.InitStyles(thm),
		HasDarkBackground: true,
		BackgroundSource:  "default",
	}
}

func TestNewModelSetsProgramContext(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)

	require.NotNil(t, m.ctx)
	require.Same(t, ctx, m.ctx)
}

func TestRenderBodyDoesNotPanicBeforeContextSync(t *testing.T) {
	ctx := newTestContext(t)
	m := NewModel(ctx)
	m.SetWidth(80)
	m.SetRow(&data.IssueData{
		Title: "Example issue",
		Body:  "Hello **world**",
	})

	require.NotPanics(t, func() {
		_ = m.renderBody()
	})
}

func blockedBy(number int, title, refs string) data.IssueData {
	return data.IssueData{
		Number: number,
		Title:  title,
		Body:   "## Parent\n\n#1\n\n## Blocked by\n\n" + refs + "\n",
		State:  "OPEN",
	}
}

var issues = []data.IssueData{
	{Number: 2, Title: "[AFK] Lay the rails", Body: "## Parent\n\n#1", State: "OPEN"},
	blockedBy(3, "[AFK] Run the train", "- #2\n- #9"),
	blockedBy(4, "[AFK] Sell tickets", "- #3"),
	blockedBy(5, "[AFK] Open the station", "- #4"),
	{Number: 7, Title: "[AFK] Paint the benches", Body: "## Parent\n\n#1", State: "OPEN"},
}

func previewOf(t *testing.T, number int) string {
	t.Helper()
	return previewIn(t, issues, number)
}

func previewIn(t *testing.T, issues []data.IssueData, number int) string {
	t.Helper()
	m := NewModel(newTestContext(t))
	m.SetWidth(80)
	a := queue.Analyze(issues, "me", nil, "owner", "repo")
	for _, i := range issues {
		if i.Number == number {
			m.SetRow(&i)
		}
	}
	m.SetDependencies(a.UpstreamTree(number), a.DownstreamTree(number))
	return m.View()
}

func lineWith(t *testing.T, view, text string) string {
	t.Helper()
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	require.Failf(t, "line not found", "no line contains %q", text)
	return ""
}

func TestPreviewDrawsTheDependencyTreesAboveTheBody(t *testing.T) {
	view := ansi.Strip(previewOf(t, 4))

	expectedInOrder := []string{
		"Blocked by",
		"└─ 🔴 #3 [AFK] Run the train",
		"   ├─ 🟢 #2 [AFK] Lay the rails",
		"   └─ ⚫ #9 closed",
		"Unblocks",
		"└─ 🔴 #5 [AFK] Open the station",
		"Parent",
	}
	at := 0
	for _, text := range expectedInOrder {
		next := strings.Index(view[at:], text)
		require.GreaterOrEqual(t, next, 0, "%q missing after offset %d in\n%s", text, at, view)
		at += next + len(text)
	}
}

func TestPreviewGreysOutClosedBlockers(t *testing.T) {
	const faintForeground = "38;2;176;179;191"
	view := previewOf(t, 3)

	require.Contains(t, lineWith(t, view, "#9 closed"), faintForeground)
	require.NotContains(t, lineWith(t, view, "#2 [AFK] Lay the rails"), faintForeground)
}

func TestPreviewOfAnIssueWithoutDependenciesHasNoTrees(t *testing.T) {
	view := ansi.Strip(previewOf(t, 7))

	require.NotContains(t, view, "Blocked by")
	require.NotContains(t, view, "Unblocks")
}

func TestPreviewLeavesOutAnEmptyTree(t *testing.T) {
	view := ansi.Strip(previewOf(t, 2))

	require.NotContains(t, view, "Blocked by")
	require.Contains(t, view, "Unblocks")
}

func TestPreviewMarksACycleAndStopsThere(t *testing.T) {
	cyclic := []data.IssueData{
		blockedBy(3, "[AFK] Run the train", "- #4"),
		blockedBy(4, "[AFK] Sell tickets", "- #3"),
	}
	view := ansi.Strip(previewIn(t, cyclic, 3))
	lines := strings.Split(view, "\n")

	blockedByAt := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Blocked by") })
	require.GreaterOrEqual(t, blockedByAt, 0, view)
	tree := lines[blockedByAt+1:]
	require.Contains(t, tree[0], "└─ ")
	require.Contains(t, tree[0], "#4 [AFK] Sell tickets")
	require.Contains(t, tree[1], "   └─ ")
	require.Contains(t, tree[1], "#3 [AFK] Run the train ↻")
	require.NotContains(t, tree[2], "─ ", "nothing is drawn beneath the cycle node")
}
