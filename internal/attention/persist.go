package attention

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/serge/cms/internal/agent"
	"github.com/serge/cms/internal/tmux"
)

// Tmux pane user-option keys for persisting attention state.
const (
	tmuxOptActivity = "@cms_activity" // activity name: "idle", "working", "waiting"
	tmuxOptSince    = "@cms_since"    // unix timestamp when activity started
)

// PersistActivitySince writes the activity transition timestamp to a tmux pane option.
func PersistActivitySince(paneID string, activity agent.Activity, since time.Time) {
	// Fire-and-forget: don't block the watcher on tmux I/O.
	go func() {
		_ = setTmuxPaneOption(paneID, tmuxOptActivity, activity.String())
		_ = setTmuxPaneOption(paneID, tmuxOptSince, strconv.FormatInt(since.Unix(), 10))
	}()
}

// ClearPersisted removes the persisted state from a tmux pane.
func ClearPersisted(paneID string) {
	go func() {
		_ = unsetTmuxPaneOption(paneID, tmuxOptActivity)
		_ = unsetTmuxPaneOption(paneID, tmuxOptSince)
	}()
}

// persistedActivity holds a recovered activity and its start time.
type persistedActivity struct {
	activity string
	since    time.Time
}

// LoadPersisted reads activity timestamps from all panes in bulk.
// Returns paneID → (activity, since) for panes that have persisted state.
func LoadPersisted(paneIDs []string) map[string]persistedActivity {
	if len(paneIDs) == 0 {
		return nil
	}

	result := map[string]persistedActivity{}

	// Build a single tmux command that reads both options for all panes.
	// tmux show-options -p doesn't support multi-pane queries, so we use
	// display-message per pane with a format string.
	for _, paneID := range paneIDs {
		out, err := tmux.Run("display-message", "-t", paneID, "-p",
			fmt.Sprintf("#{%s}\t#{%s}", tmuxOptActivity, tmuxOptSince))
		if err != nil {
			continue
		}
		fields := strings.SplitN(out, "\t", 2)
		if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
			continue
		}
		ts, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || ts == 0 {
			continue
		}
		result[paneID] = persistedActivity{
			activity: fields[0],
			since:    time.Unix(ts, 0),
		}
	}
	return result
}

func setTmuxPaneOption(paneID, key, value string) error {
	_, err := tmux.Run("set-option", "-p", "-t", paneID, key, value)
	return err
}

func unsetTmuxPaneOption(paneID, key string) error {
	_, err := tmux.Run("set-option", "-p", "-t", paneID, "-u", key)
	return err
}
