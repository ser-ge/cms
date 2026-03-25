package main

import (
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// queueEntry tracks which agent pane a picker item maps to.
type queueEntry struct {
	paneID   string
	session  string
	activity Activity
	reason   AttentionReason
	unseen   bool
	since    time.Time
}

type queueModel struct {
	picker  pickerModel
	entries []queueEntry // parallel to picker items

	// State from watcher.
	sessData  []Session
	agentData map[string]AgentStatus
	watcher   *Watcher

	done       bool
	action     *postAction
	cfg        Config
	width      int
	height     int
}

func newQueueModel(cfg Config, watcher *Watcher, width, height int) queueModel {
	m := queueModel{
		cfg:     cfg,
		watcher: watcher,
		width:   width,
		height:  height,
	}

	sessions, agents, _ := watcher.CachedState()
	if len(sessions) > 0 {
		m.sessData = sessions
		m.agentData = agents
	}
	m.rebuildPicker()
	return m
}

func (m queueModel) Init() tea.Cmd {
	return nil
}

func (m queueModel) Update(msg tea.Msg) (queueModel, tea.Cmd) {
	switch msg := msg.(type) {
	case stateMsg:
		m.sessData = msg.sessions
		m.agentData = msg.agents
		m.rebuildPicker()
		return m, nil

	case agentUpdateMsg:
		m.agentData = applyAgentUpdates(m.agentData, msg.updates)
		m.rebuildPicker()
		return m, nil

	case attentionUpdateMsg:
		m.rebuildPicker()
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.picker.width = msg.Width
		m.picker.height = msg.Height
		return m, nil
	}

	var cmd tea.Cmd
	m.picker, cmd = m.picker.Update(msg)

	if m.picker.done {
		if m.picker.chosen >= 0 && m.picker.chosen < len(m.entries) {
			entry := m.entries[m.picker.chosen]
			if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "enter" {
				// Mark attention as seen for this pane.
				m.watcher.Attention.MarkSeen(entry.paneID)
				m.action = &postAction{paneID: entry.paneID}
			}
		}
		m.done = true
	}
	return m, cmd
}

// queueItem holds sorting metadata alongside the picker item.
type queueItem struct {
	item     PickerItem
	entry    queueEntry
	sortKey  int     // lower = more urgent
	sortTime int64   // secondary sort within same priority
}

func (m *queueModel) rebuildPicker() {
	actSince := m.watcher.ActivitySince()
	attnEvents := m.watcher.Attention.Snapshot()

	// Build unseen lookup: paneID → most urgent reason.
	unseenReason := map[string]AttentionReason{}
	unseenSet := map[string]bool{}
	for _, ev := range attnEvents {
		if ev.Seen {
			continue
		}
		unseenSet[ev.PaneID] = true
		if prev, ok := unseenReason[ev.PaneID]; !ok || ev.Reason.priority() < prev.priority() {
			unseenReason[ev.PaneID] = ev.Reason
		}
	}

	// Collect all agent panes.
	var items []queueItem
	now := time.Now()

	for _, sess := range m.sessData {
		for _, win := range sess.Windows {
			for _, pane := range win.Panes {
				cs, ok := m.agentData[pane.ID]
				if !ok || !cs.Running {
					continue
				}

				since := actSince[pane.ID]
				elapsed := time.Duration(0)
				hasSince := !since.IsZero()
				if hasSince {
					elapsed = now.Sub(since)
				}

				// Build display line.
				title := sess.Name
				desc := m.buildDescription(pane, cs, elapsed, hasSince)

				filterVal := sess.Name
				if pane.Git.Branch != "" {
					filterVal += " " + pane.Git.Branch
				}
				filterVal += " " + cs.Provider.String()

				unseen := unseenSet[pane.ID]
				reason, hasReason := unseenReason[pane.ID]

				// Sort key: unseen waiting (0), unseen finished (1), working (2), idle (3).
				sortKey := 3
				if unseen && hasReason {
					sortKey = reason.priority()
				} else {
					switch cs.Activity {
					case ActivityWaitingInput:
						sortKey = 0
					case ActivityWorking:
						sortKey = 2
					case ActivityIdle:
						sortKey = 3
					}
				}

				// Within each tier: waiting = oldest first, working = longest first, idle = newest first.
				// Unknown-time items sort after known-time items within their tier.
				var sortTime int64
				if !hasSince {
					sortTime = 1<<62 - 1 // sort last within tier
				} else {
					switch {
					case sortKey <= 1:
						sortTime = since.Unix() // oldest first (ascending)
					case sortKey == 2:
						sortTime = since.Unix() // longest working first (ascending)
					default:
						sortTime = -since.Unix() // newest idle first (descending)
					}
				}

				items = append(items, queueItem{
					item: PickerItem{
						Title:       title,
						Description: desc,
						FilterValue: filterVal,
						Active:      unseen,
					},
					entry: queueEntry{
						paneID:   pane.ID,
						session:  sess.Name,
						activity: cs.Activity,
						unseen:   unseen,
						since:    since,
					},
					sortKey:  sortKey,
					sortTime: sortTime,
				})
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].sortKey != items[j].sortKey {
			return items[i].sortKey < items[j].sortKey
		}
		return items[i].sortTime < items[j].sortTime
	})

	// Build picker items and entries.
	pickerItems := make([]PickerItem, len(items))
	entries := make([]queueEntry, len(items))
	for i, qi := range items {
		pickerItems[i] = qi.item
		entries[i] = qi.entry
	}
	m.entries = entries

	// Preserve picker state across rebuilds.
	query := m.picker.Value()
	cursor := m.picker.cursor
	mode := m.picker.mode

	m.picker = newPicker("", pickerItems, m.cfg.General.EscapeChord, m.cfg.General.EscapeChordMs)
	m.picker.width = m.width
	m.picker.height = m.height
	m.picker.mode = mode
	if mode == pickerNormal {
		m.picker.input.Blur()
	}
	if query != "" {
		m.picker.input.SetValue(query)
		m.picker.applyFilter()
	}
	if cursor < m.picker.visibleCount() {
		m.picker.cursor = cursor
	}
}

func (m *queueModel) buildDescription(pane Pane, cs AgentStatus, elapsed time.Duration, hasSince bool) string {
	var parts []string

	// Git branch.
	if pane.Git.IsRepo && pane.Git.Branch != "" {
		b := pane.Git.Branch
		if pane.Git.Dirty {
			b += "*"
		}
		parts = append(parts, b)
	}

	// Provider.
	if cs.Provider != ProviderUnknown {
		parts = append(parts, cs.Provider.String())
	}

	// Context %.
	if cs.ContextSet {
		parts = append(parts, fmt.Sprintf("%d%%", cs.ContextPct))
	}

	// Activity + duration (— if no observed transition yet).
	if hasSince {
		parts = append(parts, cs.Activity.String()+" "+formatDuration(elapsed))
	} else {
		parts = append(parts, cs.Activity.String())
	}

	return joinParts(parts)
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return "<1s"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func (m queueModel) View() string {
	if len(m.entries) == 0 && m.picker.visibleCount() == 0 {
		return "  No agent sessions\n"
	}
	return m.picker.View()
}
