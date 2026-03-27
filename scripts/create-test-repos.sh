#!/usr/bin/env bash
#
# Create test repos for cms demos and integration tests.
#
# Generates 10 repos: 5 bare-repo worktree layouts and 5 normal git repos.
# All repos have multiple branches. Bare repos each get 3 feature worktrees.
#
# Usage:
#   ./scripts/create-test-repos.sh [TARGET_DIR]
#
# Default target: /tmp/cms-test-repos
#
set -euo pipefail

TARGET="${1:-/tmp/cms-test-repos}"

rm -rf "$TARGET"
mkdir -p "$TARGET"

# ── Helpers ────────────────────────────────────────────────────────────

# make_bare_project <name> <branch1> <branch2> ...
#   Creates a bare repo with a main worktree + one worktree per branch.
make_bare_project() {
  local name="$1"; shift
  local bare="$TARGET/$name/$name.git"
  local main_wt="$TARGET/$name/main"

  # Create a normal repo first, then convert to bare + worktree layout.
  local tmp="$TARGET/.tmp-$name"
  git init "$tmp" -b main >/dev/null 2>&1

  # Seed commits
  cat > "$tmp/README.md" <<EOF
# $name
Test project for cms.
EOF
  mkdir -p "$tmp/src"
  echo "package main" > "$tmp/src/main.go"
  echo 'func hello() { fmt.Println("hello from '"$name"'") }' >> "$tmp/src/main.go"
  git -C "$tmp" add -A >/dev/null 2>&1
  git -C "$tmp" commit -m "init $name" >/dev/null 2>&1

  # Second commit so there's some history
  echo "// v2" >> "$tmp/src/main.go"
  git -C "$tmp" add -A >/dev/null 2>&1
  git -C "$tmp" commit -m "add v2 comment" >/dev/null 2>&1

  # Clone as bare
  git clone --bare "$tmp" "$bare" >/dev/null 2>&1
  rm -rf "$tmp"

  # Create main worktree
  git -C "$bare" worktree add "$main_wt" main >/dev/null 2>&1

  # Create feature worktrees
  for branch in "$@"; do
    local wt="$TARGET/$name/$branch"
    git -C "$bare" worktree add -b "$branch" "$wt" main >/dev/null 2>&1

    # Make a diverging commit on each branch
    echo "// $branch work" >> "$wt/src/main.go"
    git -C "$wt" add -A >/dev/null 2>&1
    git -C "$wt" commit -m "$branch: initial work" >/dev/null 2>&1
  done

  echo "  [bare] $name  (main + $# branches)"
}

# make_bare_project_with_merged <name> <merged_branch> <branch1> ...
#   Like make_bare_project but fast-forward merges the first branch into main.
make_bare_project_with_merged() {
  local name="$1"; shift
  local merged_branch="$1"; shift

  make_bare_project "$name" "$merged_branch" "$@"

  local main_wt="$TARGET/$name/main"
  git -C "$main_wt" merge "$merged_branch" --no-edit >/dev/null 2>&1

  echo "    ↳ $merged_branch merged into main"
}

# make_normal_project <name> <branch1> <branch2> ...
#   Creates a normal (non-bare) git repo with branches.
#   Branches diverge from main but are not checked out as worktrees.
make_normal_project() {
  local name="$1"; shift
  local repo="$TARGET/$name"

  git init "$repo" -b main >/dev/null 2>&1

  # Seed commits
  cat > "$repo/README.md" <<EOF
# $name
Test project for cms.
EOF
  mkdir -p "$repo/src"
  echo "package main" > "$repo/src/main.go"
  echo 'func hello() { fmt.Println("hello from '"$name"'") }' >> "$repo/src/main.go"
  git -C "$repo" add -A >/dev/null 2>&1
  git -C "$repo" commit -m "init $name" >/dev/null 2>&1

  # Second commit for history
  echo "// v2" >> "$repo/src/main.go"
  git -C "$repo" add -A >/dev/null 2>&1
  git -C "$repo" commit -m "add v2 comment" >/dev/null 2>&1

  # Create branches with diverging commits
  for branch in "$@"; do
    git -C "$repo" checkout -b "$branch" main >/dev/null 2>&1
    echo "// $branch work" >> "$repo/src/main.go"
    git -C "$repo" add -A >/dev/null 2>&1
    git -C "$repo" commit -m "$branch: initial work" >/dev/null 2>&1
  done

  # Return to main
  git -C "$repo" checkout main >/dev/null 2>&1

  echo "  [norm] $name  (main + $# branches)"
}

# ── Repos ──────────────────────────────────────────────────────────────

echo "Creating test repos in: $TARGET"
echo ""

# --- Bare repos (5, each with 3 feature worktrees) ---

make_bare_project "webstore" \
  "feature-auth" "feature-api" "bugfix-login"

make_bare_project_with_merged "analytics" \
  "shipped-v2" "feature-dashboard" "refactor-db"

make_bare_project "platform" \
  "feat-search" "feat-export" "fix-perf"

make_bare_project "billing" \
  "feature-invoices" "feature-subscriptions" "fix-tax-calc"

make_bare_project "gateway" \
  "feat-rate-limit" "feat-oauth" "fix-timeout"

# --- Normal repos (5, each with multiple branches) ---

make_normal_project "docs-site" \
  "redesign" "add-tutorials" "fix-nav"

make_normal_project "cli-tools" \
  "feature-completions" "feature-config" "refactor-output"

make_normal_project "monitoring" \
  "add-alerts" "add-metrics" "fix-dashboard"

make_normal_project "infra" \
  "upgrade-terraform" "add-staging" "fix-dns"

make_normal_project "sdk" \
  "v2-api" "add-retry" "fix-types"

echo ""
echo "Done. Layout:"
echo ""
find "$TARGET" -maxdepth 2 -mindepth 1 -type d ! -name '.tmp-*' | sort | sed "s|$TARGET/||" | while read -r line; do
  echo "  $line"
done
echo ""
echo "To use with cms, add to your config:"
echo "  search_paths = [\"$TARGET\"]"
