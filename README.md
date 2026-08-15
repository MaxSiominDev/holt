# holt

A CLI tool for working with git worktrees, and for dragging your gitignored files
into them.

Worktrees themselves are great: a directory per branch, so switching task is `cd`.
Everything around them is the annoying part. Making one off a fresh `main` is three
commands. Getting back to the one you made last week means remembering where you put
it. And nothing untracked comes along - no `CLAUDE.local.md`, no
`.claude/settings.local.json`, no local editor config - with no warning that it did
not.

holt does the lifecycle and keeps those personal files present in every worktree,
including the ones you make later.

## Install

```sh
brew tap MaxSiominDev/tap
brew install holt
holt shell-init zsh --install   # or bash
```

Then open a new shell.

`--install` adds three lines to `~/.zshrc`, inside a block it owns, and leaves the rest
of the file alone. What it stores is `eval "$(holt shell-init zsh)"` rather than a copy
of the function, so `brew upgrade holt` upgrades the function too. Delete the block to
undo it.

The function exists because a program cannot `cd` the shell that started it. `holt new`,
`cd` and `home` print the directory they resolved and the function enters it. Everything
else works without it. `holt doctor` says whether it is loaded.

## Commands

| Command | |
| --- | --- |
| `holt new <branch>` | add a worktree and go there, branching off the default branch if the branch is new |
| `holt cd [<branch>]` | go to an existing worktree; TAB completes the branch |
| `holt home` | back to the main checkout, from anywhere in the repository |
| `holt main` | in the main checkout, switch to the default branch whatever it is called |
| `holt ls` | every worktree, how far it has drifted, whether it holds work |
| `holt rm <branch>` | remove one worktree, and its branch if the default branch has it |
| `holt rebase` | rebase this branch onto a freshly fetched default branch |
| `holt push [-f]` | push this branch to origin |
| `holt pull` | pull this branch from origin |
| `holt open` | open the merge request for this branch, or the page for raising one |
| `holt status` | `git status` for this worktree, also `holt st` |
| `holt mirror` | manage the personal files mirrored into every worktree |
| `holt doctor` | check what is set up here and what is not |

Worktrees land in `<repo>-worktrees/<branch>`, beside the main checkout. The default
branch is whatever origin says it is; there is no setting for it.

`holt rm` removes the one worktree you named and never passes `--force`, so git's own
refusal over uncommitted changes stands. Ignored files are the gap in that: git deletes
those along with the worktree, so holt lists them first.

`holt push -f` is what a rebase leaves you needing. It sends `--force-with-lease` and
`--force-if-includes` together, because the lease on its own is satisfied by any fetch
at all, including a background one from your editor, and then the push goes straight
over someone else's commit.

`holt open` asks `gh` or `glab` whether a request is already open from the branch and
opens that one. Neither is required: without them you get the page for raising a
request, which needs no token. On a host named after neither GitHub nor GitLab, say
which it is with `git config holt.forge gitlab`.

## Mirroring your own files

Once, in the main checkout:

```sh
holt mirror add CLAUDE.local.md
holt mirror add .claude/settings.local.json
holt mirror add '.claude/skills/*-local'
```

Paths are relative to the repository root and may be globs. Every worktree then gets a
symlink to the real file in the main checkout, the existing ones now and later ones as
git creates them, through a `post-checkout` hook. Edit it anywhere, you edit the one
file.

Nothing is ever overwritten: a real file where a symlink would go is left alone and
reported. `holt mirror sync` re-applies the list, repairing links that were deleted or
went stale and backfilling worktrees older than the path itself.

## Requirements

git 2.41 or newer, which is what the formula installs. Older git works, except that
`holt ls` cannot fill in the ahead/behind columns and says so.

## License

MIT
