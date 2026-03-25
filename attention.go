package main

import (
	"sync"
	"time"
)

// AttentionReason describes why a pane needs attention.
type AttentionReason int

const (
	AttentionWaiting  AttentionReason = iota // agent needs user input
	AttentionFinished                        // agent just completed work (Working → Idle)
)

func (r AttentionReason) String() string {
	switch r {
	case AttentionWaiting:
		return "waiting"
	case AttentionFinished:
		return "finished"
	default:
		return "unknown"
	}
}

// attentionPriority returns sort priority (lower = more urgent).
func (r AttentionReason) priority() int {
	switch r {
	case AttentionWaiting:
		return 0
	case AttentionFinished:
		return 1
	default:
		return 99
	}
}

// AttentionEvent records that a pane needs attention.
type AttentionEvent struct {
	PaneID    string
	Reason    AttentionReason
	Timestamp time.Time
	Seen      bool
}

// AttentionQueue is a thread-safe ordered collection of attention events.
type AttentionQueue struct {
	mu     sync.Mutex
	events []AttentionEvent
}

// Add inserts or updates an attention event for a pane+reason pair.
// Deduplicates: if the same pane+reason already exists and is unseen,
// the timestamp is kept (don't reset the clock). If seen, it's replaced.
func (q *AttentionQueue) Add(paneID string, reason AttentionReason) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, ev := range q.events {
		if ev.PaneID == paneID && ev.Reason == reason {
			if !ev.Seen {
				return // already tracking, keep existing timestamp
			}
			// Was seen — refresh it as unseen.
			q.events[i].Timestamp = time.Now()
			q.events[i].Seen = false
			return
		}
	}
	q.events = append(q.events, AttentionEvent{
		PaneID:    paneID,
		Reason:    reason,
		Timestamp: time.Now(),
	})
}

// Remove deletes all events for a pane with the given reason.
func (q *AttentionQueue) Remove(paneID string, reason AttentionReason) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = filterEvents(q.events, func(ev AttentionEvent) bool {
		return !(ev.PaneID == paneID && ev.Reason == reason)
	})
}

// RemovePane deletes all events for a pane.
func (q *AttentionQueue) RemovePane(paneID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = filterEvents(q.events, func(ev AttentionEvent) bool {
		return ev.PaneID != paneID
	})
}

// MarkSeen marks all events for a pane as seen.
func (q *AttentionQueue) MarkSeen(paneID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.events {
		if q.events[i].PaneID == paneID {
			q.events[i].Seen = true
		}
	}
}

// UnseenCount returns the number of unseen events.
func (q *AttentionQueue) UnseenCount() int {
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
func (q *AttentionQueue) UnseenForPane(paneID string) bool {
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
func (q *AttentionQueue) Snapshot() []AttentionEvent {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]AttentionEvent, len(q.events))
	copy(out, q.events)
	return out
}

// Prune removes events older than maxAge.
func (q *AttentionQueue) Prune(maxAge time.Duration) {
	q.mu.Lock()
	defer q.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	q.events = filterEvents(q.events, func(ev AttentionEvent) bool {
		return ev.Timestamp.After(cutoff)
	})
}

func filterEvents(events []AttentionEvent, keep func(AttentionEvent) bool) []AttentionEvent {
	n := 0
	for _, ev := range events {
		if keep(ev) {
			events[n] = ev
			n++
		}
	}
	return events[:n]
}
