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
