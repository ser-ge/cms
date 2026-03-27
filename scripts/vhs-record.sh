#!/usr/bin/env bash
#
# Record a cms demo GIF using VHS in an isolated tmux environment.
#
# Usage:
#   ./scripts/vhs-record.sh [options]
#   ./scripts/vhs-record.sh --manual          # drop into shell for hand-recording
#
# Options:
#   --output <file>       Output file (default: demo.gif)
#   --theme <name>        VHS theme (default: Catppuccin Mocha)
#   --width <px>          Terminal width in pixels (default: 1200)
#   --height <px>         Terminal height in pixels (default: 600)
#   --font-size <n>       Font size (default: 16)
#   --tape <file>         Custom tape template (default: scripts/demo.tape)
#   --manual              Set up environment then drop into interactive shell
#
set -euo pipefail

# ── Defaults ─────────────────────────────────────────────────────────
OUTPUT="demo.gif"
THEME="Catppuccin Mocha"
WIDTH=1200
HEIGHT=600
FONT_SIZE=16
TAPE_TEMPLATE=""
MANUAL=false

# ── Parse args ───────────────────────────────────────────────────────
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)     OUTPUT="$2";    shift 2 ;;
    --theme)      THEME="$2";     shift 2 ;;
    --width)      WIDTH="$2";     shift 2 ;;
    --height)     HEIGHT="$2";    shift 2 ;;
    --font-size)  FONT_SIZE="$2"; shift 2 ;;
    --tape)       TAPE_TEMPLATE="$2"; shift 2 ;;
    --manual)     MANUAL=true;    shift ;;
    -h|--help)
      sed -n '2,/^$/{ s/^# //; s/^#//; p }' "$0"
      exit 0 ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
TAPE_TEMPLATE="${TAPE_TEMPLATE:-$SCRIPT_DIR/demo.tape}"

# ── Prerequisites ────────────────────────────────────────────────────
for cmd in vhs go tmux; do
  if ! command -v "$cmd" &>/dev/null; then
    echo "Error: $cmd not found on PATH" >&2
    exit 1
  fi
done

# ── Isolated environment ─────────────────────────────────────────────
RUN_ID="$(head -c6 /dev/urandom | xxd -p)"
HARNESS_ROOT="/tmp/cms-vhs-$RUN_ID"
REPOS="$HARNESS_ROOT/repos"
CONFIG_DIR="$HARNESS_ROOT/config"
TMUX_SERVER="cms-vhs-$RUN_ID"
TMUX_CONF="$HARNESS_ROOT/tmux.conf"
T="tmux -L $TMUX_SERVER -f $TMUX_CONF"

cleanup() {
  $T kill-server 2>/dev/null || true
  rm -rf "$HARNESS_ROOT"
}
trap cleanup EXIT

# ── Create test repos ────────────────────────────────────────────────
"$SCRIPT_DIR/create-test-repos.sh" "$REPOS"
REPOS="$(cd "$REPOS" && pwd -P)"

# ── Build cms ────────────────────────────────────────────────────────
CMS_BIN="$HARNESS_ROOT/cms"
echo ""
echo "Building cms..."
go build -o "$CMS_BIN" "$SCRIPT_DIR/.."
echo "  $CMS_BIN"

# ── Install config ─────────────────────────────────────────────────
mkdir -p "$CONFIG_DIR"
sed "s|/tmp/cms-demo/repos|$REPOS|" "$SCRIPT_DIR/../config.toml" > "$CONFIG_DIR/config.toml"
echo ""
echo "Config: $CONFIG_DIR/config.toml"

# ── Minimal tmux config ─────────────────────────────────────────────
cat > "$TMUX_CONF" <<'TMUXEOF'
set -g default-terminal "screen-256color"
set -g base-index 0
set -g pane-base-index 0
set -g status off
TMUXEOF

# ── Start isolated tmux server ───────────────────────────────────────
$T new-session  -d -s webstore  -c "$REPOS/webstore/main"        -x 160 -y 40
$T split-window -h -t webstore  -c "$REPOS/webstore/feature-auth"
$T split-window -h -t webstore  -c "$REPOS/webstore/feature-api"

$T new-session  -d -s analytics -c "$REPOS/analytics/main"
$T split-window -h -t analytics -c "$REPOS/analytics/shipped-v2"
$T split-window -h -t analytics -c "$REPOS/analytics/feature-dashboard"

$T new-session  -d -s platform  -c "$REPOS/platform/main"
$T split-window -h -t platform  -c "$REPOS/platform/feat-search"
$T split-window -h -t platform  -c "$REPOS/platform/fix-perf"

echo ""
echo "Tmux sessions:"
$T list-sessions 2>&1 | sed 's/^/  /'

# ── Compute tmux socket path ────────────────────────────────────────
TMUX_SOCKET="/tmp/tmux-$(id -u)/$TMUX_SERVER"

# ── Manual mode: set up repos + config, use real tmux + shell ────────
if $MANUAL; then
  # Create sessions on the user's default tmux server (not isolated)
  echo ""
  echo "Creating tmux sessions on default server..."
  tmux new-session  -d -s webstore  -c "$REPOS/webstore/main"             2>/dev/null || true
  tmux split-window -h -t webstore  -c "$REPOS/webstore/feature-auth"     2>/dev/null || true
  tmux split-window -h -t webstore  -c "$REPOS/webstore/feature-api"      2>/dev/null || true

  tmux new-session  -d -s analytics -c "$REPOS/analytics/main"             2>/dev/null || true
  tmux split-window -h -t analytics -c "$REPOS/analytics/shipped-v2"       2>/dev/null || true
  tmux split-window -h -t analytics -c "$REPOS/analytics/feature-dashboard" 2>/dev/null || true

  tmux new-session  -d -s platform  -c "$REPOS/platform/main"             2>/dev/null || true
  tmux split-window -h -t platform  -c "$REPOS/platform/feat-search"      2>/dev/null || true
  tmux split-window -h -t platform  -c "$REPOS/platform/fix-perf"         2>/dev/null || true

  echo ""
  echo "Tmux sessions (default server):"
  tmux list-sessions 2>&1 | sed 's/^/  /'

  # Write a helper env file that the user can source
  ENV_FILE="$HARNESS_ROOT/env.sh"
  cat > "$ENV_FILE" <<ENVEOF
export CMS_CONFIG_DIR="$HARNESS_ROOT/config"
export PATH="$HARNESS_ROOT:\$PATH"
ENVEOF

  ENV_FILE_FISH="$HARNESS_ROOT/env.fish"
  cat > "$ENV_FILE_FISH" <<ENVEOF
set -gx CMS_CONFIG_DIR "$HARNESS_ROOT/config"
fish_add_path --prepend "$HARNESS_ROOT"
ENVEOF

  echo ""
  echo "Environment ready. To use cms with the demo config:"
  echo ""
  echo "  # fish"
  echo "  source $ENV_FILE_FISH"
  echo ""
  echo "  # bash/zsh"
  echo "  source $ENV_FILE"
  echo ""
  echo "Then run:"
  echo "  cms"
  echo ""
  echo "To hand-record with VHS:"
  echo "  vhs record > my-demo.tape"
  echo "  vhs my-demo.tape"
  echo ""
  echo "To clean up when done:"
  echo "  tmux kill-session -t webstore"
  echo "  tmux kill-session -t analytics"
  echo "  tmux kill-session -t platform"
  echo "  rm -rf $HARNESS_ROOT"
  echo ""

  # Don't clean up on exit in manual mode — user controls lifecycle
  trap - EXIT
  exit 0
fi

# ── Generate tape from template ──────────────────────────────────────
TAPE_FILE="$HARNESS_ROOT/demo.tape"

# Make output path absolute if relative
case "$OUTPUT" in
  /*) ;; # already absolute
  *)  OUTPUT="$(pwd)/$OUTPUT" ;;
esac

sed \
  -e "s|__OUTPUT__|$OUTPUT|g" \
  -e "s|__THEME__|$THEME|g" \
  -e "s|__WIDTH__|$WIDTH|g" \
  -e "s|__HEIGHT__|$HEIGHT|g" \
  -e "s|__FONT_SIZE__|$FONT_SIZE|g" \
  -e "s|__CMS_CONFIG_DIR__|$HARNESS_ROOT/config|g" \
  -e "s|__CMS_TMUX_SOCKET__|$TMUX_SOCKET|g" \
  -e "s|__BIN_DIR__|$HARNESS_ROOT|g" \
  "$TAPE_TEMPLATE" > "$TAPE_FILE"

echo ""
echo "Tape: $TAPE_FILE"
echo "Output: $OUTPUT"
echo ""

# ── Record ───────────────────────────────────────────────────────────
vhs "$TAPE_FILE"

echo ""
echo "Done: $OUTPUT"
