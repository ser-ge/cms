package attention

import "time"

// PersistedActivity holds a recovered activity and its start time.
// Exported wrapper for cross-package access.
type PersistedActivity struct {
	Activity string
	Since    time.Time
}

// LoadPersistedExported reads activity timestamps from all panes in bulk,
// returning exported types for cross-package use.
func LoadPersistedExported(paneIDs []string) map[string]PersistedActivity {
	raw := LoadPersisted(paneIDs)
	if raw == nil {
		return nil
	}
	result := make(map[string]PersistedActivity, len(raw))
	for k, v := range raw {
		result[k] = PersistedActivity{
			Activity: v.activity,
			Since:    v.since,
		}
	}
	return result
}
