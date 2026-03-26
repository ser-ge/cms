#!/usr/bin/env bash
#
# Create emulated project repos with bare-repo worktree layouts for testing cms.
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

# Helper: create a bare repo with worktrees.
#   make_project <name> <branch1> <branch2> ...
make_project() {
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

  echo "  $name  (main + $# branches)"
}

# Helper: create a project with a merged branch (integrated into main).
make_project_with_merged() {
  local name="$1"; shift
  local merged_branch="$1"; shift

  make_project "$name" "$merged_branch" "$@"

  local main_wt="$TARGET/$name/main"

  # Fast-forward merge the branch into main so it shows as integrated
  git -C "$main_wt" merge "$merged_branch" --no-edit >/dev/null 2>&1

  echo "    ↳ $merged_branch merged into main"
}

echo "Creating test repos in: $TARGET"
echo ""

# --- Projects ---

# project_a: simple multi-feature project
make_project "project_a" "feature-auth" "feature-api" "bugfix-login"

# project_b: has a merged branch + active ones
make_project_with_merged "project_b" "shipped-v2" "feature-dashboard" "refactor-db"

# project_c: single worktree (just main)
make_project "project_c"

# project_d: many branches to test scrolling/filtering
make_project "project_d" \
  "feat-search" "feat-export" "feat-import" "feat-notifications" \
  "fix-perf" "fix-memory" "chore-deps" "chore-ci"

echo ""
echo "Done. Layout:"
echo ""
find "$TARGET" -maxdepth 2 -mindepth 1 -type d ! -name '.tmp-*' | sort | sed "s|$TARGET/||" | while read -r line; do
  echo "  $line"
done
echo ""
echo "To use with cms, add to your config:"
echo "  search_paths = [\"$TARGET\"]"
