package tmux

// CapturePaneBottom captures the visible content of a tmux pane.
func CapturePaneBottom(paneID string) (string, error) {
	return Run("capture-pane", "-t", paneID, "-p", "-J")
}
