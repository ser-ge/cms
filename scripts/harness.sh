#!/usr/bin/env bash
#
# Integration harness: runs real cms commands in an isolated tmux + config
# environment backed by test worktree repos.
#
# Usage:
#   ./scripts/harness.sh [options] [section...]
#
# Options:
#   --agents    Launch real claude instances in some panes
#
# Examples:
#   ./scripts/harness.sh                          # default: worktrees
#   ./scripts/harness.sh sessions,worktrees
#   ./scripts/harness.sh --agents worktrees       # with real claude agents
#   ./scripts/harness.sh --agents dash
#
set -euo pipefail

WITH_AGENTS=false
SECTIONS=""
for arg in "$@"; do
  case "$arg" in
    --agents) WITH_AGENTS=true ;;
    *)        SECTIONS="$arg" ;;
  esac
done
SECTIONS="${SECTIONS:-worktrees}"

HARNESS_ROOT="/tmp/cms-harness"
REPOS="$HARNESS_ROOT/repos"
CONFIG_DIR="$HARNESS_ROOT/config/cms"
TMUX_SERVER="cms-harness"
TMUX_CONF="$HARNESS_ROOT/tmux.conf"
T="tmux -L $TMUX_SERVER -f $TMUX_CONF"

# ── Cleanup ────────────────────────────────────────────────────────────
cleanup() { $T kill-server 2>/dev/null || true; }
trap cleanup EXIT
cleanup

# ── Create test repos ──────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
"$SCRIPT_DIR/create-test-repos.sh" "$REPOS"

# Resolve symlinks (macOS /tmp → /private/tmp)
REPOS="$(cd "$REPOS" && pwd -P)"

# ── Build cms ──────────────────────────────────────────────────────────
CMS_BIN="$HARNESS_ROOT/cms"
echo ""
echo "Building cms..."
go build -o "$CMS_BIN" "$SCRIPT_DIR/.."
echo "  $CMS_BIN"

# ── Write isolated config ─────────────────────────────────────────────
mkdir -p "$CONFIG_DIR"
cat > "$CONFIG_DIR/config.toml" <<EOF
[general]
search_paths = [
  { path = "$REPOS", max_depth = 2 },
]

[finder]
include = ["sessions", "projects", "worktrees", "queue"]
EOF
echo ""
echo "Config: $CONFIG_DIR/config.toml"

# ── Minimal tmux config (ignore user's tmux.conf) ─────────────────────
cat > "$TMUX_CONF" <<'TMUXEOF'
set -g default-terminal "screen-256color"
set -g base-index 0
set -g pane-base-index 0
set -g status off
TMUXEOF

# ── Start isolated tmux server ─────────────────────────────────────────

# project_a: 3 panes in different worktrees
$T new-session  -d -s project_a -c "$REPOS/project_a/main"      -x 160 -y 40
$T split-window -t project_a   -c "$REPOS/project_a/feature-auth"
$T split-window -t project_a   -c "$REPOS/project_a/feature-api"

# project_b: merged branch + active ones
$T new-session  -d -s project_b -c "$REPOS/project_b/main"
$T split-window -t project_b   -c "$REPOS/project_b/shipped-v2"
$T split-window -t project_b   -c "$REPOS/project_b/feature-dashboard"

# project_d: many worktrees
$T new-session  -d -s project_d -c "$REPOS/project_d/main"
$T split-window -t project_d   -c "$REPOS/project_d/feat-search"
$T split-window -t project_d   -c "$REPOS/project_d/fix-perf"

# ── Optionally launch claude agents ───────────────────────────────────
if $WITH_AGENTS; then
  echo ""
  echo "Launching claude agents..."

  # project_a/feature-auth: claude working on auth
  $T send-keys -t project_a:.1 \
    "claude -p 'Implement user authentication with JWT tokens. Add login/logout endpoints and middleware.'" Enter

  # project_a/feature-api: claude working on API
  $T send-keys -t project_a:.2 \
    "claude -p 'Add REST API endpoints for CRUD operations on the main resource.'" Enter

  # project_b/feature-dashboard: claude working on dashboard
  $T send-keys -t project_b:.2 \
    "claude -p 'Build a dashboard view that shows project status and recent activity.'" Enter

  # Give agents a moment to start
  sleep 2
  echo "  Agents launched in: project_a/feature-auth, project_a/feature-api, project_b/feature-dashboard"
fi

echo ""
echo "Tmux sessions:"
$T list-sessions 2>&1 | sed 's/^/  /'
echo ""
echo "Panes:"
$T list-panes -a -F '  #{session_name}:#{window_index}.#{pane_index}  #{pane_current_path}  #{pane_current_command}' 2>&1

# ── Attach and run cms ─────────────────────────────────────────────────
# Set up env in pane 0 so cms uses our isolated config.
$T send-keys -t project_a:.0 \
  "export XDG_CONFIG_HOME='$HARNESS_ROOT/config' PATH='$HARNESS_ROOT:\$PATH'" Enter
$T send-keys -t project_a:.0 "cms $SECTIONS" Enter

echo ""
echo "Attaching to harness tmux (Ctrl-b d to detach, q to quit cms)..."
echo "Re-run cms inside with: cms $SECTIONS"
echo ""

# Attach — this hands control to the user.
$T attach -t project_a
