
# holt

A CLI tool for working with git worktrees, and for dragging your gitignored files
into them.

## Install

```sh
brew tap MaxSiominDev/holt
brew install holt
holt shell-init zsh --install   # or bash
```

Then open a new shell.

## Commands

| Command | |
| --- | --- |
| `holt new <branch>` | add a worktree and go there: an existing branch as it stands, one origin has tracked, otherwise a new branch off the default |
| `holt cd [<branch>]` | go to an existing worktree; TAB completes the branch |
| `holt home` | back to the main checkout, from anywhere in the repository |
| `holt main` | in the main checkout, switch to the default branch whatever it is called |
| `holt ls` | every worktree, how far it has drifted, whether it holds work |
| `holt rm <branch>` | remove one worktree, and its branch if the default branch has it |
| `holt rebase` | rebase this branch onto a freshly fetched default branch |
| `holt push [-f]` | push this branch to origin |
| `holt pull` | pull this branch from origin |
| `holt open` | open the request for this branch, or the page for raising one |
| `holt st` | `git status` for this worktree |
| `holt mirror` | manage the personal files mirrored into every worktree |
| `holt doctor` | check what is set up here and what is not |
| `holt shell-init <shell>` | print the shell function that lets holt change directory |

Worktrees land in `<repo>-worktrees/<branch>`, beside the main checkout. The default
branch is whatever origin says it is; there is no setting for it.

## Mirroring your own files

Once, in the main checkout:

```sh
holt mirror add CLAUDE.local.md
holt mirror add .claude/settings.local.json
holt mirror add '.claude/skills/*-local'
```

You can edit those files anywhere - you edit the one file.

## Requirements

git 2.41 or newer, which is what the formula installs

## License

MIT