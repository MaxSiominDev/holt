# Changelog

What changed in each release of holt. The format is
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the numbering is
[semantic versioning](https://semver.org/spec/v2.0.0.html). The section at the top holds
what has landed since the last tag, and closing it under a version number is the last
thing done before that version is tagged.

## [Unreleased]

### Added

- `holt new`, `holt cd`, `holt home` and `holt ls`: worktrees under
  `<repo>-worktrees/<branch>`, beside the main checkout, and a shell function that moves the
  shell into them.
- `holt rm`, `holt rebase`, `holt push`, `holt pull`, `holt st` and `holt open`: the day to
  day of a branch, run from its worktree rather than from the main checkout.
- `holt main`: switch the main checkout to the default branch, whatever origin calls it.
- `holt mirror`: gitignored personal files symlinked into every worktree and put back by a
  post-checkout hook, since git carries none of them over on its own.
- `holt doctor`: what is set up in a repository and what is not.
- `holt shell-init`: the function `holt cd` and `holt new` need, written into `~/.zshrc` or
  `~/.bashrc` on request.

[Unreleased]: https://github.com/MaxSiominDev/holt/commits/main
