# Queue view

The Queue view lists the open issues of the current repository in the order
wiq would work them, and the issues that are waiting on you. wiq is the work
issue queue, `~/.claude/scripts/work-issue-queue.sh`, which picks the next
runnable issue and hands it to a Claude worker.

## Opening it

Press `s` to cycle through the views: PRs, Issues, Queue, then Repo if that
view is enabled. To start gh-dash on the Queue view, set it as the default:

```yaml
defaults:
  view: queue
```

The view only covers the repository you started gh-dash from. It fetches every
open issue in that repository, however many there are. If you start gh-dash
outside a GitHub repository, both tabs stay empty.

## Tabs

The Queue view has two tabs. You can't rename them or add sections to this view
in the config.

The **Queue** tab shows what wiq would run. Candidates come first: the Next
issue, then the other Ready issues, then the Blocked ones, each group sorted by
number. Parked issues are listed after them.

The **Needs me** tab shows the issues only you can move forward. It has every
Parked issue, plus the HITL issues that block at least one candidate, directly
or through other issues. An HITL issue only shows up here when its body has a
`## Parent` heading and it is unassigned or you are one of its assignees. The
tab is sorted by the Unblocks
column, highest first, so the issue that frees the most work is at the top.
Issues with the same count are in number order.

## States

The State column shows one of these:

| State | Shown as | Meaning |
| --- | --- | --- |
| Next | 🟢 Next | The lowest-numbered candidate with no open blockers. wiq picks this one next. |
| Ready | 🟢 Ready | Any other candidate with no open blockers. |
| Blocked | 🔴 Blocked by #12, #15 (HITL) | A candidate with open blockers. Blockers that are HITL issues get `(HITL)` after them, and parked ones get `(parked)`. |
| Parked | 🟠 Parked | The issue would be a candidate, but it has the `blocked` label. The `/wiq` command parks an issue this way when a run fails on it, and you can add the label yourself. |
| HITL | 🟣 HITL | An issue that needs a human, because its title contains `HITL` or it has the `hitl` label. It only appears in Needs me, and only when it blocks a candidate, its body has a `## Parent` heading, and it is unassigned or assigned to you. |

An issue is a candidate when all of these are true, which are the same checks
wiq makes:

- It has none of the labels `blocked`, `wontfix`, `needs-info`, `needs-triage`
  or `hitl`. Labels must match exactly.
- Its title doesn't contain `HITL`, in upper or lower case.
- Its body contains a `## Parent` heading.
- Nobody is assigned, or you are one of the assignees.

Blockers come from the `## Blocked by` section of the issue body. gh-dash reads
`#12`, `owner/repo#12` and full issue URLs there. "None - can start immediately"
means the issue has no blockers. A blocker in the same repository counts as
closed when it isn't among the open issues. Blockers in other repositories are
looked up in one batched GitHub request, and if that request fails they count as
open.

## Columns

The Queue view adds three columns before the usual issue columns.

State is described above.

Unblocks counts the candidates that are waiting on the issue, directly or
through other open issues in the same repository. Finishing an issue with a high count frees the most work.

Worktree shows `⏵ worktree` when wiq has already created a worktree for the
issue. wiq puts it next to your checkout, as `../<repo>.wiq-issue-<number>`.
This usually means a run started on the issue and didn't finish, so the next
run will reuse that worktree.

## Preview

With the preview open, two trees appear above the issue body. Blocked by shows
what the issue waits on, and Unblocks shows what is waiting on it. Both trees go
down every level. Each node has the same icon as its state in the table. A ⚫
node is a closed blocker, drawn in a faint color. A ⚪ node is an open issue
that isn't in either tab, such as an issue in another repository. Issues in
other repositories aren't expanded. `↻` marks an issue that already appears
higher up in the same branch, so the tree has a cycle there and doesn't expand
it again.

Press `tab` to move a highlight through the tree nodes, and `enter` to jump to
the highlighted one. If the issue is in the current tab, gh-dash selects its
row. If it is in the other tab, gh-dash switches tabs and selects it there.
Otherwise, it opens the issue in your browser. These keys are the builtins
`nextDependency` and `jumpToDependency`, so you can rebind them under
`keybindings.issues`.

## Refreshing

The view reloads on the `defaults.refetchIntervalMinutes` interval, and when you
press `r`. After you change a label or a `## Blocked by` section on GitHub,
press `r` to see the result.

## Keybindings

Each row is an issue, so the usual issue actions work here, along with your
custom commands under `keybindings.issues`. Their templates get `RepoName`,
`IssueNumber`, `IssueTitle`, `Author` and `RepoPath`. There is no separate
`keybindings.queue` section.

These two commands help with the queue. `U` and `W` aren't bound to anything
in the Issues or Queue views. Change them if you have already used them for
something else.

```yaml
keybindings:
  issues:
    - key: U
      name: unpark (remove blocked label)
      command: >
        gh issue edit {{.IssueNumber}} -R {{.RepoName}} --remove-label blocked
    - key: W
      name: run wiq in background
      command: >
        dir="{{.RepoPath}}"; cd "${dir/#\~/$HOME}" || exit;
        perl -MPOSIX -e 'setsid; exec @ARGV'
        bash ~/.claude/scripts/work-issue-queue.sh
        </dev/null >"/tmp/wiq-$(date +%s).log" 2>&1 &
```

`U` removes the `blocked` label from the selected issue. After the next refresh
it moves from Parked back to Ready, Next or Blocked.

`W` starts wiq from the repository folder and returns to gh-dash right away.

The first line moves to the repository folder. `RepoPath` comes from your
`repoPaths` config when the repository is listed there, and those paths often
start with `~`. The shell doesn't expand a `~` inside quotes, so the line
replaces a leading `~` with `$HOME` before `cd`. The quotes stay so that paths
with spaces work. This works in bash and zsh. If the folder doesn't exist, `cd`
fails and gh-dash shows its error.

gh-dash runs custom commands through your `$SHELL` and waits for the command to
finish, so the command puts wiq in the background with `&`. `setsid` (through
`perl`, because macOS has no `setsid` command) starts wiq in its own session, so
it keeps running when you quit gh-dash or close the terminal. wiq's progress
lines go to `/tmp/wiq-<timestamp>.log`, the same name the `/wiq` command uses
for its log. Worker transcripts still go to `~/.wiq/logs/`. To follow a run,
use:

```sh
tail -f "$(ls -t /tmp/wiq-*.log | head -1)"
```

wiq exits right away when tracked files in the working tree have uncommitted
changes. gh-dash doesn't show that error, so check the log if nothing seems to
happen. The key needs a selected row, because issue keybindings run against the
current row.

## Where the rules come from

The rules copy the issue picker in wiq. `wiq_candidates` in
`~/.claude/scripts/wiq-lib.sh` is the source of truth, and gh-dash repeats its
labels, title check and body checks as constants in `internal/queue`. If you
change the rules in wiq, change them in gh-dash too. Parked and HITL issues
exist only in gh-dash, because wiq skips them.

One difference is that wiq only looks at the first 100 open issues
(`gh issue list --limit 100`), and gh-dash reads all of them. In a repository
with more than 100 open issues, gh-dash can show candidates that wiq won't see.
