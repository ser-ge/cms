# Release Strategy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Set up semantic release workflow with goreleaser, GitHub Actions, Homebrew tap, and a `/release` skill.

**Architecture:** Git tags are the single source of truth. goreleaser reads the tag, builds cross-platform binaries, generates a changelog from conventional commits, creates a GitHub Release, and pushes a Homebrew formula. A local `/release` skill orchestrates the tag-and-push workflow.

**Tech Stack:** goreleaser, GitHub Actions, MIT license, conventional commits

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `main.go` | Modify | Add `version` var + `--version`/`-V` flag handling |
| `LICENSE` | Create | MIT license |
| `.goreleaser.yaml` | Create | Build matrix, changelog grouping, Homebrew tap |
| `.github/workflows/release.yml` | Create | Tag-push CI: test + goreleaser |
| `CLAUDE.md` | Modify | Add conventional commit rules |
| `README.md` | Modify | Add badges + install section |
| `.claude/skills/release/SKILL.md` | Create | `/release` skill definition |
| `Makefile` | Modify | Inject dev version in `dev` target |

---

### Task 1: Add version variable and `--version` flag

**Files:**
- Modify: `main.go:1-2` (add var), `main.go:29-37` (add flag check before internal dispatch)

- [ ] **Step 1: Add version variable at package level**

Add after the import block (line 19), before the `jumpCandidate` struct:

```go
// version is set at build time via ldflags.
var version = "dev"
```

- [ ] **Step 2: Add `--version`/`-V` flag handling**

Insert after `args := os.Args[1:]` (line 29) and before the internal commands block (line 31). This must run before config loading since `--version` shouldn't require a valid config:

```go
	// Version flag — runs before config loading.
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-V") {
		fmt.Println("cms", version)
		return
	}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build -o /dev/null ./...`
Expected: Clean build, exit 0

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: add --version/-V flag with build-time version injection"
```

---

### Task 2: Add MIT license

**Files:**
- Create: `LICENSE`

- [ ] **Step 1: Create LICENSE file**

```
MIT License

Copyright (c) 2025 Serge

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 2: Commit**

```bash
git add LICENSE
git commit -m "chore: add MIT license"
```

---

### Task 3: Create goreleaser config

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create `.goreleaser.yaml`**

```yaml
version: 2

builds:
  - main: .
    binary: cms
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos:
      - darwin
      - linux
    goarch:
      - amd64
      - arm64

archives:
  - format: tar.gz
    name_template: "cms_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - README.md
      - LICENSE

changelog:
  sort: asc
  groups:
    - title: Features
      regexp: '^feat'
    - title: Bug Fixes
      regexp: '^fix'
    - title: Performance
      regexp: '^perf'
    - title: Other
      order: 999

brews:
  - repository:
      owner: ser-ge
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"
    homepage: https://github.com/ser-ge/cms
    description: Tmux session picker and dashboard with Claude and Codex awareness
    license: MIT
    install: |
      bin.install "cms"
```

- [ ] **Step 2: Validate config syntax**

Run: `go install github.com/goreleaser/goreleaser/v2@latest && goreleaser check`
Expected: output contains `config is valid` or similar success message

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "chore: add goreleaser config for cross-platform releases"
```

---

### Task 4: Create GitHub Actions release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create workflow directory**

Run: `mkdir -p .github/workflows`

- [ ] **Step 2: Create `.github/workflows/release.yml`**

```yaml
name: Release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod

      - name: Run tests
        run: go test ./...

      - uses: goreleaser/goreleaser-action@v6
        with:
          version: "~> v2"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "chore: add GitHub Actions release workflow"
```

---

### Task 5: Update Makefile with version injection

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Update `dev` target**

Replace the current `dev` target to inject a dev version string:

```makefile
CONFIG := $(HOME)/.config/cms/config.toml
BACKUP := $(HOME)/.config/cms/config.toml.bak
VERSION := dev-$(shell git describe --tags --always --dirty 2>/dev/null || echo unknown)

.PHONY: dev restore

dev:
	go install -ldflags "-X main.version=$(VERSION)" .
	@if [ -f "$(CONFIG)" ]; then mv "$(CONFIG)" "$(BACKUP)"; echo "backed up $(CONFIG) → $(BACKUP)"; fi
	cms config init

restore:
	@if [ -f "$(BACKUP)" ]; then mv "$(BACKUP)" "$(CONFIG)"; echo "restored $(CONFIG)"; else echo "no backup found at $(BACKUP)"; exit 1; fi
```

- [ ] **Step 2: Verify it works**

Run: `make dev && cms --version`
Expected: `cms dev-<hash>` (e.g. `cms dev-1ce0d3b-dirty`)

- [ ] **Step 3: Commit**

```bash
git add Makefile
git commit -m "chore: inject dev version string in Makefile"
```

---

### Task 6: Add conventional commit rules to CLAUDE.md

**Files:**
- Modify: `CLAUDE.md` (append after the Development section at end of file)

- [ ] **Step 1: Append commit convention section**

Add at the end of CLAUDE.md:

```markdown
## Commit Convention

All commits use conventional commit format. This drives changelog generation.

```
feat: ...       New feature (bumps minor)
fix: ...        Bug fix (bumps patch)
refactor: ...   Code restructuring (no version bump)
docs: ...       Documentation only
test: ...       Tests only
chore: ...      Build, CI, tooling
perf: ...       Performance improvement
```

- Breaking changes: add `!` after type — `feat!: remove --legacy flag`
- Scope is optional: `feat(worktree): add land command`
- Subject: lowercase, imperative, no period
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: add conventional commit rules to CLAUDE.md"
```

---

### Task 7: Update README with badges and install section

**Files:**
- Modify: `README.md:1-3` (add badges after title, add install section before Commands)

- [ ] **Step 1: Add badges after the title line**

Replace the first 3 lines of README.md (the `# cms` header and description) with:

```markdown
# cms

[![Release](https://img.shields.io/github/v/release/ser-ge/cms)](https://github.com/ser-ge/cms/releases)
[![Go](https://img.shields.io/github/go-mod-go-version/ser-ge/cms)](https://go.dev/)
[![License](https://img.shields.io/github/license/ser-ge/cms)](LICENSE)

`cms` is a tmux session picker and dashboard with Claude and Codex awareness.

## Install

### Homebrew

```bash
brew install ser-ge/tap/cms
```

### Go

```bash
go install github.com/ser-ge/cms@latest
```

### Binary

Download from [GitHub Releases](https://github.com/ser-ge/cms/releases).
```

- [ ] **Step 2: Verify README renders**

Eyeball the markdown. The badges won't resolve until the first release is published, but the markdown syntax should be correct.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: add version badges and install section to README"
```

---

### Task 8: Create `/release` skill

**Files:**
- Create: `.claude/skills/release/SKILL.md`

- [ ] **Step 1: Create skill directory**

Run: `mkdir -p .claude/skills/release`

- [ ] **Step 2: Create `.claude/skills/release/SKILL.md`**

````markdown
---
name: release
description: Create a new semantic version release — bumps version, generates changelog, tags, and pushes to trigger CI
---

# Release

Create a new release of cms. Usage: `/release patch`, `/release minor`, or `/release major`.

## Procedure

### 1. Preflight checks

Run ALL of these. If any fail, stop and report the issue.

```bash
# Must be on main
[ "$(git branch --show-current)" = "main" ] && echo "OK: on main" || echo "FAIL: not on main"

# Must be clean
git diff --quiet && git diff --staged --quiet && echo "OK: clean" || echo "FAIL: dirty tree"

# Must be up to date with remote
git fetch origin main
git diff --quiet main origin/main && echo "OK: up to date" || echo "FAIL: local/remote diverged"

# Tests must pass
go test ./...
```

### 2. Calculate next version

Read the latest tag:

```bash
git describe --tags --abbrev=0 2>/dev/null || echo "none"
```

If no tag exists, the next version is `v0.1.0` regardless of bump type.

Otherwise, parse the tag as `vMAJOR.MINOR.PATCH` and bump the requested component:
- `patch`: increment PATCH
- `minor`: increment MINOR, reset PATCH to 0
- `major`: increment MAJOR, reset MINOR and PATCH to 0

### 3. Generate changelog preview

List commits since the last tag (or all commits if first release):

```bash
git log $(git describe --tags --abbrev=0 2>/dev/null)..HEAD --oneline
# If no tags: git log --oneline
```

Group by conventional commit type:
- **Features**: commits starting with `feat`
- **Bug Fixes**: commits starting with `fix`
- **Performance**: commits starting with `perf`
- **Other**: everything else

Present the grouped changelog and the calculated version to the user. Wait for explicit confirmation before proceeding.

### 4. Tag and push

Only after user confirms:

```bash
git tag <version>
git push origin main --tags
```

### 5. Report

Print:
- The tag that was created
- Link: `https://github.com/ser-ge/cms/actions` (to watch the release workflow)
- Remind user that goreleaser will create the GitHub Release and update the Homebrew tap automatically

## Edge cases

- **No commits since last tag**: abort with "Nothing to release"
- **Dirty working tree**: abort with "Commit or stash changes first"
- **Not on main**: abort with "Switch to main first"
- **First release**: use v0.1.0, changelog is all commits
- **Never force-push, never amend, never release without confirmation**
````

- [ ] **Step 3: Commit**

```bash
git add .claude/skills/release/SKILL.md
git commit -m "feat: add /release skill for semantic version releases"
```

---

### Task 9: Verify end-to-end (dry run)

This task validates the full setup without actually pushing a tag.

- [ ] **Step 1: Verify build with version injection**

Run: `go build -ldflags "-X main.version=v0.1.0" -o /tmp/cms-test . && /tmp/cms-test --version && rm /tmp/cms-test`
Expected: `cms v0.1.0`

- [ ] **Step 2: Verify goreleaser dry run**

Run: `goreleaser release --snapshot --clean --skip=publish`
Expected: Builds complete for all 4 targets (darwin/amd64, darwin/arm64, linux/amd64, linux/arm64). Archives created in `dist/`.

- [ ] **Step 3: Clean up dist**

Run: `rm -rf dist/`

- [ ] **Step 4: Verify all files are committed**

Run: `git status`
Expected: Clean working tree

- [ ] **Step 5: Add `dist/` to `.gitignore`**

goreleaser creates a `dist/` directory during builds. Add it to `.gitignore`:

Check if `.gitignore` exists:
```bash
cat .gitignore 2>/dev/null || echo "no .gitignore"
```

Add `dist/` to `.gitignore` (create the file if needed, or append).

- [ ] **Step 6: Commit**

```bash
git add .gitignore
git commit -m "chore: add dist/ to gitignore (goreleaser output)"
```
