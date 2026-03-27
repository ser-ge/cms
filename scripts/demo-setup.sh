#!/usr/bin/env bash
#
# Set up a demo environment for recording cms.
#
# Creates test repos, builds cms, writes an isolated cms config
# (via CMS_CONFIG_DIR — does not touch XDG_CONFIG_HOME), loads a
# minimal tmux config with navigation/split/cms bindings (no theme
# or plugins), and creates demo sessions on your default tmux server.
#
# A tmux server will be started automatically if one isn't running.
# Your shell config (starship, fish, etc.) is preserved as-is.
#
# Usage:
#   ./scripts/demo-setup.sh [options]
#
# Options:
#   --keep-repos          Reuse existing repos if present (skip creation)
#   --agents              Launch real Claude agents in some panes
#   --cleanup             Tear down demo sessions, tmux env, and temp files
#
# Outputs:
#   /tmp/cms-demo/          Root directory
#   /tmp/cms-demo/cms       Built binary
#   /tmp/cms-demo/config/   cms config (CMS_CONFIG_DIR points here)
#   /tmp/cms-demo/env.fish  Source this in fish  (for direct CLI use)
#   /tmp/cms-demo/env.sh    Source this in bash/zsh
#
# The script also sets CMS_CONFIG_DIR and PATH in the tmux global
# environment so that cms bindings (C-s C-s, etc.) work in popups
# without needing to source env files first.
#
set -euo pipefail

KEEP_REPOS=false
WITH_AGENTS=false
CLEANUP=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --keep-repos) KEEP_REPOS=true; shift ;;
    --agents)     WITH_AGENTS=true; shift ;;
    --cleanup)    CLEANUP=true; shift ;;
    -h|--help)
      sed -n '2,/^$/{ s/^# //; s/^#//; p }' "$0"
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

if $CLEANUP; then
  echo "Cleaning up demo environment..."
  for sess in webstore analytics platform; do
    tmux kill-session -t "$sess" 2>/dev/null && echo "  killed session $sess" || true
  done
  tmux set-environment -gu CMS_CONFIG_DIR 2>/dev/null || true
  tmux set-environment -gu CMS_DEMO_ACTIVE 2>/dev/null || true
  # Remove demo binary from tmux PATH.
  TMUX_PATH="$(tmux show-environment -g PATH 2>/dev/null | sed 's/^PATH=//' || true)"
  TMUX_PATH="$(echo "$TMUX_PATH" | tr ':' '\n' | grep -v '/tmp/cms-demo' | tr '\n' ':' | sed 's/:$//')"
  [[ -n "$TMUX_PATH" ]] && tmux set-environment -g PATH "$TMUX_PATH" || true
  if [[ -d /tmp/cms-demo ]]; then
    rm -rf /tmp/cms-demo
    echo "  removed /tmp/cms-demo"
  fi
  echo "Done."
  exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEMO_ROOT="/tmp/cms-demo"
REPOS="$DEMO_ROOT/repos"
CONFIG_DIR="$DEMO_ROOT/config"

# ── Create test repos ────────────────────────────────────────────────
if $KEEP_REPOS && [[ -d "$REPOS" ]]; then
  echo "Reusing existing repos at $REPOS"
else
  "$SCRIPT_DIR/create-test-repos.sh" "$REPOS"
fi
REPOS="$(cd "$REPOS" && pwd -P)"

# ── Build cms ────────────────────────────────────────────────────────
echo ""
echo "Building cms..."
go build -o "$DEMO_ROOT/cms" "$SCRIPT_DIR/.."
echo "  $DEMO_ROOT/cms"

# ── Install config ──────────────────────────────────────────────────
mkdir -p "$CONFIG_DIR"
# Copy the checked-in config template, rewriting the repos path.
sed "s|/tmp/cms-demo/repos|$REPOS|" "$SCRIPT_DIR/../config.toml" > "$CONFIG_DIR/config.toml"

# ── Create tmux sessions on default server ───────────────────────────
echo ""
echo "Creating tmux sessions..."
for sess in webstore analytics platform; do
  tmux kill-session -t "$sess" 2>/dev/null || true
done

tmux new-session  -d -s webstore  -c "$REPOS/webstore/main"
tmux split-window -h -t webstore  -c "$REPOS/webstore/feature-auth"
tmux split-window -h -t webstore  -c "$REPOS/webstore/feature-api"

tmux new-session  -d -s analytics -c "$REPOS/analytics/main"
tmux split-window -h -t analytics -c "$REPOS/analytics/shipped-v2"
tmux split-window -h -t analytics -c "$REPOS/analytics/feature-dashboard"

tmux new-session  -d -s platform  -c "$REPOS/platform/main"
tmux split-window -h -t platform  -c "$REPOS/platform/feat-search"
tmux split-window -h -t platform  -c "$REPOS/platform/fix-perf"

tmux list-sessions 2>&1 | sed 's/^/  /'

# ── Configure tmux for demo ───────────────────────────────────────────
# Set env vars globally so popup commands (display-popup -E "cms") inherit them.
tmux set-environment -g CMS_CONFIG_DIR "$CONFIG_DIR"
tmux set-environment -g CMS_DEMO_ACTIVE 1
# Prepend demo binary to PATH for tmux-spawned shells.
TMUX_PATH="$(tmux show-environment -g PATH 2>/dev/null | sed 's/^PATH=//' || echo "$PATH")"
tmux set-environment -g PATH "$DEMO_ROOT:$TMUX_PATH"
# Load demo-specific tmux bindings.
tmux source-file "$SCRIPT_DIR/demo-tmux.conf"

# ── Optionally launch Claude agents ─────────────────────────────────
if $WITH_AGENTS; then
  echo ""
  echo "Launching Claude agents..."

  tmux send-keys -t webstore:.1 \
    "claude -p 'Implement user authentication with JWT tokens. Add login/logout endpoints and middleware.'" Enter

  tmux send-keys -t webstore:.2 \
    "claude -p 'Add REST API endpoints for CRUD operations on the main resource.'" Enter

  tmux send-keys -t analytics:.2 \
    "claude -p 'Build a dashboard view that shows project status and recent activity.'" Enter

  sleep 2
  echo "  Agents launched in: webstore/feature-auth, webstore/feature-api, analytics/feature-dashboard"
fi

# ── Write env files ──────────────────────────────────────────────────
cat > "$DEMO_ROOT/env.sh" <<ENVEOF
export CMS_CONFIG_DIR="$DEMO_ROOT/config"
export PATH="$DEMO_ROOT:\$PATH"
ENVEOF

cat > "$DEMO_ROOT/env.fish" <<ENVEOF
set -gx CMS_CONFIG_DIR "$DEMO_ROOT/config"
fish_add_path --prepend "$DEMO_ROOT"
ENVEOF

echo ""
echo "Ready. Source the env for your shell:"
echo ""
echo "  source $DEMO_ROOT/env.fish    # fish"
echo "  source $DEMO_ROOT/env.sh      # bash/zsh"
echo ""
echo "Then: cms"
