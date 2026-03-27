#!/usr/bin/env bash
#
# Set up a demo environment for recording cms.
#
# Creates test repos, builds cms, writes an isolated config, and
# creates tmux sessions on your default server. Your real shell
# and tmux config are used as-is.
#
# Usage:
#   ./scripts/demo-setup.sh [options]
#
# Options:
#   --sections <list>     cms finder sections (default: sessions,worktrees)
#   --keep-repos          Reuse existing repos if present (skip creation)
#   --agents              Launch real Claude agents in some panes
#   --cleanup             Tear down demo sessions and temp files
#
# Outputs:
#   /tmp/cms-demo/          Root directory
#   /tmp/cms-demo/cms       Built binary
#   /tmp/cms-demo/env.fish  Source this in fish
#   /tmp/cms-demo/env.sh    Source this in bash/zsh
#
set -euo pipefail

SECTIONS="sessions,worktrees"
KEEP_REPOS=false
WITH_AGENTS=false
CLEANUP=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --sections)   SECTIONS="$2"; shift 2 ;;
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
  for sess in project_a project_b project_d; do
    tmux kill-session -t "$sess" 2>/dev/null && echo "  killed session $sess" || true
  done
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
CONFIG_DIR="$DEMO_ROOT/config/cms"

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

# ── Write config ─────────────────────────────────────────────────────
mkdir -p "$CONFIG_DIR"

TOML_SECTIONS=""
IFS=',' read -ra SECTS <<< "$SECTIONS"
for s in "${SECTS[@]}"; do
  TOML_SECTIONS+="\"$s\", "
done
TOML_SECTIONS="[${TOML_SECTIONS%, }]"

cat > "$CONFIG_DIR/config.toml" <<EOF
[general]
search_paths = [
  { path = "$REPOS", max_depth = 2 },
]

[finder]
include = $TOML_SECTIONS
EOF

# ── Create tmux sessions on default server ───────────────────────────
echo ""
echo "Creating tmux sessions..."
for sess in project_a project_b project_d; do
  tmux kill-session -t "$sess" 2>/dev/null || true
done

tmux new-session  -d -s project_a -c "$REPOS/project_a/main"
tmux split-window -t project_a   -c "$REPOS/project_a/feature-auth"
tmux split-window -t project_a   -c "$REPOS/project_a/feature-api"

tmux new-session  -d -s project_b -c "$REPOS/project_b/main"
tmux split-window -t project_b   -c "$REPOS/project_b/shipped-v2"
tmux split-window -t project_b   -c "$REPOS/project_b/feature-dashboard"

tmux new-session  -d -s project_d -c "$REPOS/project_d/main"
tmux split-window -t project_d   -c "$REPOS/project_d/feat-search"
tmux split-window -t project_d   -c "$REPOS/project_d/fix-perf"

tmux list-sessions 2>&1 | sed 's/^/  /'

# ── Optionally launch Claude agents ─────────────────────────────────
if $WITH_AGENTS; then
  echo ""
  echo "Launching Claude agents..."

  tmux send-keys -t project_a:.1 \
    "claude -p 'Implement user authentication with JWT tokens. Add login/logout endpoints and middleware.'" Enter

  tmux send-keys -t project_a:.2 \
    "claude -p 'Add REST API endpoints for CRUD operations on the main resource.'" Enter

  tmux send-keys -t project_b:.2 \
    "claude -p 'Build a dashboard view that shows project status and recent activity.'" Enter

  sleep 2
  echo "  Agents launched in: project_a/feature-auth, project_a/feature-api, project_b/feature-dashboard"
fi

# ── Write env files ──────────────────────────────────────────────────
cat > "$DEMO_ROOT/env.sh" <<ENVEOF
export XDG_CONFIG_HOME="$DEMO_ROOT/config"
export PATH="$DEMO_ROOT:\$PATH"
ENVEOF

cat > "$DEMO_ROOT/env.fish" <<ENVEOF
set -gx XDG_CONFIG_HOME "$DEMO_ROOT/config"
fish_add_path --prepend "$DEMO_ROOT"
ENVEOF

echo ""
echo "Ready. Source the env for your shell:"
echo ""
echo "  source $DEMO_ROOT/env.fish    # fish"
echo "  source $DEMO_ROOT/env.sh      # bash/zsh"
echo ""
echo "Then: cms $SECTIONS"
