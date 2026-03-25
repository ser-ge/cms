package attention

import (
	"sync"
	"time"
)

// Reason describes why a pane needs attention.
type Reason int

const (
	Waiting  Reason = iota // agent needs user input
	Finished               // agent just completed work (Working -> Idle)
)

func (r Reason) String() string {
	switch r {
	case Waiting:
		return "waiting"
	case Finished:
		return "finished"
	default:
		return "unknown"
	}
}

// priority returns sort priority (lower = more urgent).
func (r Reason) priority() int {
	switch r {
	case Waiting:
		return 0
	case Finished:
		return 1
	default:
		return 99
	}
}

// Event records that a pane needs attention.
type Event struct {
	PaneID    string
	Reason    Reason
	Timestamp time.Time
	Seen      bool
}

// Queue is a thread-safe ordered collection of attention events.
type Queue struct {
	mu     sync.Mutex
	events []Event
}

// Add inserts or updates an attention event for a pane+reason pair.
// Deduplicates: if the same pane+reason already exists and is unseen,
// the timestamp is kept (don't reset the clock). If seen, it's replaced.
func (q *Queue) Add(paneID string, reason Reason) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, ev := range q.events {
		if ev.PaneID == paneID && ev.Reason == reason {
			if !ev.Seen {
				return // already tracking, keep existing timestamp
			}
			// Was seen -- refresh it as unseen.
			q.events[i].Timestamp = time.Now()
			q.events[i].Seen = false
			return
		}
	}
	q.events = append(q.events, Event{
		PaneID:    paneID,
		Reason:    reason,
		Timestamp: time.Now(),
	})
}

// Remove deletes all events for a pane with the given reason.
func (q *Queue) Remove(paneID string, reason Reason) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = filterEvents(q.events, func(ev Event) bool {
		return !(ev.PaneID == paneID && ev.Reason == reason)
	})
}

// RemovePane deletes all events for a pane.
func (q *Queue) RemovePane(paneID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = filterEvents(q.events, func(ev Event) bool {
		return ev.PaneID != paneID
	})
}

// MarkSeen marks all events for a pane as seen.
func (q *Queue) MarkSeen(paneID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.events {
		if q.events[i].PaneID == paneID {
			q.events[i].Seen = true
		}
	}
}

// UnseenCount returns the number of unseen events.
func (q *Queue) UnseenCount() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := 0
	for _, ev := range q.events {
		if !ev.Seen {
			n++
		}
	}
	return n
}

// UnseenForPane returns true if the pane has any unseen attention events.
func (q *Queue) UnseenForPane(paneID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, ev := range q.events {
		if ev.PaneID == paneID && !ev.Seen {
			return true
		}
	}
	return false
}

// Snapshot returns a copy of all events (for rendering).
func (q *Queue) Snapshot() []Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Event, len(q.events))
	copy(out, q.events)
	return out
}

// Prune removes events older than maxAge.
func (q *Queue) Prune(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	q.events = filterEvents(q.events, func(ev Event) bool {
		return ev.Timestamp.After(cutoff)
	})
}

func filterEvents(events []Event, keep func(Event) bool) []Event {
	n := 0
	for _, ev := range events {
		if keep(ev) {
			events[n] = ev
			n++
		}
	}
	return events[:n]
}
