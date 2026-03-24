package main

import (
	"fmt"
	"testing"
)

func TestFzfMatching(t *testing.T) {
	items := []PickerItem{
		{Title: "cms", FilterValue: "cms /Users/serge/projects/cms"},
		{Title: "dotfiles", FilterValue: "dotfiles /Users/serge/projects/dotfiles"},
		{Title: "gather-md.git", FilterValue: "gather-md.git /Users/serge/projects/gather-md.git/main"},
		{Title: "notes", FilterValue: "notes /Users/serge/projects/notes"},
	}

	tests := []struct {
		query   string
		wantHit string
	}{
		{"gather", "gather-md.git"},
		{"gat", "gather-md.git"},
		{"cms", "cms"},
		{"dot", "dotfiles"},
		{"notes", "notes"},
		{"gather main", "gather-md.git"},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			p := newPicker("test", items, "", 0)
			p.input.SetValue(tt.query)
			p.applyFilter()

			fmt.Printf("Query: %q → %d matches\n", tt.query, len(p.matches))
			for i, m := range p.matches {
				fmt.Printf("  [%d] score=%d idx=%d title=%q\n", i, m.Score, m.Index, items[m.Index].Title)
			}

			found := false
			for _, m := range p.matches {
				if items[m.Index].Title == tt.wantHit {
					found = true
				}
			}
			if !found {
				t.Errorf("query %q: expected %q in results, got %d matches", tt.query, tt.wantHit, len(p.matches))
			}
		})
	}
}
