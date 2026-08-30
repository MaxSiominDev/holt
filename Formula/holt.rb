# The installed copy lives in MaxSiominDev/homebrew-holt; this one is edited
# alongside the source so the two stay in sync.
class Holt < Formula
  desc "Git worktree helper that mirrors personal files into every worktree"
  homepage "https://github.com/MaxSiominDev/holt"
  url "https://github.com/MaxSiominDev/holt/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
  license "MIT"
  head "https://github.com/MaxSiominDev/holt.git", branch: "main"

  depends_on "go" => :build
  # "holt ls" needs the ahead-behind atom from git 2.41, and Apple's git is not
  # something to count on tracking it.
  depends_on "git"

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
    generate_completions_from_executable(bin/"holt", "completion")
  end

  def caveats
    <<~EOS
      holt cd, holt home and holt new move your shell, which a program cannot do
      on its own. Add the function that does it, then open a new shell:

        holt shell-init zsh --install     # or bash

      That writes a five-line block into ~/.zshrc and leaves the rest alone.
      It stores the eval line rather than the function, so "brew upgrade holt"
      upgrades the function too, and the line does nothing once holt is
      uninstalled.

      "holt doctor" reports what is set up in a repository and what is not.
    EOS
  end

  test do
    assert_match "holt #{version}", shell_output("#{bin}/holt --version")
    # holt's own wording, which git's "not a git repository" does not contain.
    assert_match "holt has to run inside", shell_output("#{bin}/holt ls 2>&1", 1)
    assert_match "holt()", shell_output("#{bin}/holt shell-init zsh")
  end
end
