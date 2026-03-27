# Release Strategy Design

**Date:** 2026-03-27
**Status:** Draft

## Overview

Standardize the `cms` release process with semantic versioning, automated builds, and three installation methods: `go install`, prebuilt binaries via GitHub Releases, and Homebrew.

## Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| First version | `v0.1.0` | Signals early stage, room for breaking changes |
| Version source of truth | Git tags | Everything reads from the tag — no version files |
| Release branch | `main` | Single developer, no staging needed |
| Version bump trigger | Semi-automatic (`/release patch\|minor\|major`) | Control over when/what, changelog auto-generated |
| Commit convention | Conventional commits, enforced via CLAUDE.md | Better changelogs, low overhead |
| Build/release tool | goreleaser | Go standard, handles binaries + changelog + Homebrew |
| CI | GitHub Actions | Tag push triggers goreleaser |
| License | MIT | Standard for Go CLI tools |
| Config compatibility | Forward-compatible, no version field | New fields get defaults, old fields kept 2 minor versions |

## 1. Version Injection

A single variable in `main.go`:

```go
var version = "dev"
```

- goreleaser sets it via `ldflags -X main.version={{.Version}}` at release time
- `go install .` / `go build .` gets `dev` (correct for development)
- `--version` / `-V` flag prints the version and exits

No version files, no constants to sync. The git tag is the single source of truth.

## 2. Conventional Commits

Added to CLAUDE.md and enforced for all commits:

```
feat: ...       New feature (bumps minor)
fix: ...        Bug fix (bumps patch)
refactor: ...   Code restructuring (no version bump)
docs: ...       Documentation only
test: ...       Tests only
chore: ...      Build, CI, tooling
perf: ...       Performance improvement

Breaking changes: add `!` after type — feat!: remove --legacy flag
Scope is optional: feat(worktree): add land command
Subject: lowercase, imperative, no period.
```

## 3. goreleaser Configuration

`.goreleaser.yaml` at project root:

- **Builds:** macOS (arm64, amd64) + Linux (arm64, amd64). No Windows (tmux doesn't run there).
- **ldflags:** `-X main.version={{.Version}}`
- **Archives:** `tar.gz` containing binary, README.md, LICENSE
- **Changelog:** Grouped by conventional commit type (Features, Bug Fixes, Other). Generated from commits since last tag.
- **GitHub Release:** Created automatically with grouped changelog as body.
- **Homebrew tap:** Pushes formula to `ser-ge/homebrew-tap`. Formula installs prebuilt binary.

## 4. GitHub Actions Workflow

`.github/workflows/release.yml` — triggers on tag push `v*`:

1. Checkout code
2. Set up Go 1.24
3. Run `go test ./...` (release fails if tests fail)
4. Run goreleaser with `GITHUB_TOKEN` + `HOMEBREW_TAP_TOKEN`

~25 lines. No other CI workflows initially.

## 5. Homebrew Tap

Separate repo: `ser-ge/homebrew-tap`

goreleaser auto-generates and pushes the formula on each release. Users install with:

```bash
brew install ser-ge/tap/cms
```

## 6. Config Version Compatibility

No config version field. No migration scripts.

- **New fields** — get defaults in code. Missing TOML keys use zero values.
- **Renamed fields** — old name kept working for 2 minor versions, debug log warns.
- **Removed fields** — unknown TOML keys silently ignored.
- **`cms config init`** — writes current version's defaults. Never overwrites existing config.
- **Breaking config changes** — changelog documents migration steps explicitly.

## 7. README Updates

Add to top of README.md:

- **Badges:** Latest Release, Go Version, License (shields.io)
- **Install section** with all 3 methods (Homebrew, go install, binary download)

## 8. `/release` Skill

Local skill at `.claude/skills/release/SKILL.md`. Invoked as `/release patch|minor|major`.

**Steps:**

1. **Preflight** — assert on `main`, clean tree, `go test ./...` passes
2. **Calculate version** — read last tag, bump requested component
3. **Changelog preview** — group commits since last tag by conventional type
4. **Confirm** — show version + changelog, wait for user approval
5. **Tag + push** — `git tag <version> && git push origin main --tags`
6. **Report** — print tag and link to GitHub Actions run

**Edge cases:**

- First release (`v0.1.0`): no previous tag, changelog is all commits
- No commits since last tag: abort with message
- Dirty working tree: abort with message
- Not on main: abort with message

Never force-pushes, never amends, never releases without confirmation.

## 9. Manual GitHub Setup (one-time)

### Create Homebrew tap repo

1. github.com/new → name: `homebrew-tap` (under `ser-ge`)
2. Public, initialize with README

### Create PAT for goreleaser

1. GitHub → Settings → Developer Settings → Personal Access Tokens → Fine-grained tokens
2. Name: `goreleaser-homebrew`
3. Repository access: Only `ser-ge/homebrew-tap`
4. Permissions: Contents (Read and Write)

### Add secret to cms repo

1. `ser-ge/cms` → Settings → Secrets and variables → Actions
2. New secret: `HOMEBREW_TAP_TOKEN` = the PAT

## Files to Create/Modify

| File | Action |
|------|--------|
| `main.go` | Add `var version = "dev"` + `--version`/`-V` flag |
| `.goreleaser.yaml` | New — build matrix, changelog, homebrew |
| `.github/workflows/release.yml` | New — tag push → goreleaser |
| `LICENSE` | New — MIT license |
| `CLAUDE.md` | Add conventional commit rules |
| `README.md` | Add badges + install section |
| `.claude/skills/release/SKILL.md` | New — `/release` skill |
| `Makefile` | Update `dev` target (optional: inject dev version) |
