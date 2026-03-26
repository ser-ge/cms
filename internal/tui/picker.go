package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

// PickerItem is anything that can be shown in the picker.
type PickerItem struct {
	Title       string
	Description string
	FilterValue string // searched by fuzzy match
	Active      bool   // true for active sessions (rendered distinctly)
}

// fzfMatch holds a matched item with its score and matched positions.
type fzfMatch struct {
	Index     int   // index into original items
	Score     int   // fzf score (higher = better)
	MatchedAt []int // character positions that matched
}

// pickerModel is a reusable fuzzy-find picker with input at the bottom.
type pickerMode int

const (
	pickerInsert pickerMode = iota // typing in filter
	pickerNormal                   // vim-style navigation with j/k
)

// PickerAction is a normal-mode command the parent should handle.
type PickerAction int

const (
	PickerNoAction PickerAction = iota
	PickerActionDelete          // x: delete/close the selected item
)

type pickerModel struct {
	items   []PickerItem
	matches []fzfMatch // current fuzzy matches (nil = show all)
	input   textinput.Model
	cursor  int
	scroll  int // first visible item index (items render from scroll upward)
	mode    pickerMode

	// Escape chord (e.g. "jk" or "jj") detection.
	escapeChord   string // two-char chord, empty to disable
	escapeChordMs int    // timeout in ms
	chordPending  bool   // first char of chord was typed, waiting for second
	width         int
	height        int
	title         string
	chosen        int          // index into items of chosen item, -1 if none
	action        PickerAction // normal-mode action for parent to handle
	done          bool
	confirm       string // non-empty = showing "y/n" confirmation prompt
	slab          *util.Slab // reusable memory for fzf algo
}

func newPicker(title string, items []PickerItem, escapeChord string, escapeChordMs int) pickerModel {
	ti := textinput.New()
	ti.Prompt = "> "
	ti.Placeholder = "Type to filter..."
	ti.Focus()
	ti.CharLimit = 128

	algo.Init("default")
	m := pickerModel{
		items:         items,
		input:         ti,
		title:         title,
		chosen:        -1,
		escapeChord:   escapeChord,
		escapeChordMs: escapeChordMs,
		slab:          util.MakeSlab(100*1024, 2048),
	}
	m.applyFilter()
	return m
}

func (m pickerModel) Value() string {
	return m.input.Value()
}

// resetWith replaces items while preserving query, cursor, and mode.
func (m pickerModel) resetWith(items []PickerItem, escapeChord string, escapeChordMs int) pickerModel {
	query := m.Value()
	cursor := m.cursor
	mode := m.mode
	width := m.width
	height := m.height

	m = newPicker("", items, escapeChord, escapeChordMs)
	m.width = width
	m.height = height
	m.mode = mode
	if mode == pickerNormal {
		m.input.Blur()
	}
	if query != "" {
		m.input.SetValue(query)
		m.applyFilter()
	}
	if cursor < m.visibleCount() {
		m.cursor = cursor
	}
	return m
}

type chordTimeoutMsg struct{}

func (m pickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m pickerModel) Update(msg tea.Msg) (pickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case chordTimeoutMsg:
		// Chord timed out -- flush the pending first char to the input.
		if m.chordPending && m.mode == pickerInsert && len(m.escapeChord) == 2 {
			m.chordPending = false
			m.input.SetValue(m.input.Value() + string(m.escapeChord[0]))
			m.applyFilter()
		}
		return m, nil

	case tea.KeyMsg:
		// Confirmation prompt intercepts all keys.
		if m.confirm != "" {
			switch msg.String() {
			case "y", "Y":
				m.chosen = m.selectedItem()
				m.action = PickerActionDelete
				m.confirm = ""
			default:
				m.confirm = ""
			}
			return m, nil
		}

		// Global keys (both modes).
		switch msg.String() {
		case "enter":
			if m.chordPending {
				m.chordPending = false
			}
			sel := m.selectedItem()
			if sel >= 0 {
				m.chosen = sel
				m.done = true
			}
			return m, nil
		case "ctrl+c":
			m.done = true
			m.chosen = -1
			return m, nil
		}

		if m.mode == pickerNormal {
			return m.updateNormal(msg)
		}
		return m.updateInsert(msg)
	}

	// Pass to text input in insert mode.
	if m.mode == pickerInsert && !m.chordPending {
		var cmd tea.Cmd
		prev := m.input.Value()
		m.input, cmd = m.input.Update(msg)
		if m.input.Value() != prev {
			m.applyFilter()
		}
		return m, cmd
	}
	return m, nil
}

func (m *pickerModel) available() int {
	a := m.height - 2
	if a < 1 {
		a = 1
	}
	return a
}

func (m pickerModel) updateInsert(msg tea.KeyMsg) (pickerModel, tea.Cmd) {
	key := msg.String()

	// Chord detection: two-key sequence to exit insert mode.
	if len(m.escapeChord) == 2 {
		first := string(m.escapeChord[0])
		second := string(m.escapeChord[1])

		if m.chordPending {
			m.chordPending = false
			if key == second {
				// Chord complete -- switch to normal mode.
				m.mode = pickerNormal
				m.input.Blur()
				return m, nil
			}
			// Not the second char -- flush the buffered first char, then process this key.
			m.input.SetValue(m.input.Value() + first)
			m.applyFilter()
			// Fall through to handle the current key normally.
		} else if key == first {
			// Start chord -- buffer this key, wait for the next.
			m.chordPending = true
			return m, tea.Tick(time.Duration(m.escapeChordMs)*time.Millisecond, func(time.Time) tea.Msg {
				return chordTimeoutMsg{}
			})
		}
	}

	switch key {
	case "esc":
		m.mode = pickerNormal
		m.input.Blur()
		return m, nil
	case "ctrl+k", "ctrl+p":
		if m.cursor < m.visibleCount()-1 {
			m.cursor++
		}
		m.ensureVisible(m.available())
		return m, nil
	case "ctrl+j", "ctrl+n":
		if m.cursor > 0 {
			m.cursor--
		}
		m.ensureVisible(m.available())
		return m, nil
	}

	// Pass to text input.
	var cmd tea.Cmd
	prev := m.input.Value()
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != prev {
		m.applyFilter()
		m.scroll = 0
	}
	return m, cmd
}

func (m pickerModel) updateNormal(msg tea.KeyMsg) (pickerModel, tea.Cmd) {
	switch msg.String() {
	case "k", "up":
		if m.cursor < m.visibleCount()-1 {
			m.cursor++
		}
	case "j", "down":
		if m.cursor > 0 {
			m.cursor--
		}
	case "g":
		m.cursor = m.visibleCount() - 1
	case "G":
		m.cursor = 0
	case "i", "/":
		m.mode = pickerInsert
		m.input.Focus()
		return m, textinput.Blink
	case "x":
		if m.selectedItem() >= 0 {
			m.confirm = "close? y/n"
		}
		return m, nil
	case "esc", "q":
		m.done = true
		m.chosen = m.selectedItem()
	}
	m.ensureVisible(m.available())
	return m, nil
}

// ensureVisible adjusts scroll so the cursor is within the visible window.
func (m *pickerModel) ensureVisible(available int) {
	if available <= 0 {
		return
	}
	// Cursor must be >= scroll (bottom of window) and < scroll + available (top).
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	}
	if m.cursor >= m.scroll+available {
		m.scroll = m.cursor - available + 1
	}
}

func (m *pickerModel) applyFilter() {
	query := strings.TrimSpace(m.input.Value())
	if query == "" {
		m.matches = nil
		m.cursor = 0
		m.scroll = 0
		return
	}

	// Smart case: case-insensitive unless query has uppercase.
	caseSensitive := false
	for _, r := range query {
		if unicode.IsUpper(r) {
			caseSensitive = true
			break
		}
	}

	// fzf-style: split on spaces, each token must match (AND logic).
	tokens := strings.Fields(query)

	m.matches = nil
	for i, item := range m.items {
		text := util.ToChars([]byte(item.FilterValue))
		totalScore := 0
		var allPositions []int
		matched := true

		for _, token := range tokens {
			pattern := []rune(token)
			result, positions := algo.FuzzyMatchV2(caseSensitive, true, true, &text, pattern, true, m.slab)
			if result.Start < 0 {
				matched = false
				break
			}
			totalScore += result.Score
			if positions != nil {
				allPositions = append(allPositions, *positions...)
			}
		}

		if matched {
			m.matches = append(m.matches, fzfMatch{
				Index:     i,
				Score:     totalScore,
				MatchedAt: allPositions,
			})
		}
	}

	// Sort by score descending (best match first = index 0 = bottom of picker).
	// Active items (sessions) are prioritized over inactive (projects).
	sort.Slice(m.matches, func(i, j int) bool {
		ai := m.items[m.matches[i].Index].Active
		aj := m.items[m.matches[j].Index].Active
		if ai != aj {
			return ai // sessions before projects
		}
		return m.matches[i].Score > m.matches[j].Score
	})

	if m.cursor >= m.visibleCount() {
		m.cursor = max(0, m.visibleCount()-1)
	}
}

func (m *pickerModel) visibleCount() int {
	if m.matches != nil {
		return len(m.matches)
	}
	return len(m.items)
}

// selectedItem returns the index into m.items of the currently selected item, or -1.
func (m *pickerModel) selectedItem() int {
	count := m.visibleCount()
	if count == 0 || m.cursor < 0 || m.cursor >= count {
		return -1
	}
	if m.matches != nil {
		return m.matches[m.cursor].Index
	}
	return m.cursor
}

func (m pickerModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	var b strings.Builder

	available := m.available()

	// Build visible items list.
	type displayItem struct {
		idx       int // index into m.items
		matchIdxs []int
	}
	var visible []displayItem

	if m.matches != nil {
		for _, match := range m.matches {
			visible = append(visible, displayItem{idx: match.Index, matchIdxs: match.MatchedAt})
		}
	} else {
		for i := range m.items {
			visible = append(visible, displayItem{idx: i})
		}
	}

	// Fixed viewport: items stay in place, only highlight moves.
	// Scroll adjusts only when cursor leaves the visible area.
	start := m.scroll
	end := start + available
	if end > len(visible) {
		end = len(visible)
		start = max(0, end-available)
	}

	rendered := end - start
	for i := 0; i < available-rendered; i++ {
		b.WriteString("\n")
	}

	for i := end - 1; i >= start; i-- {
		item := m.items[visible[i].idx]
		isSelected := i == m.cursor

		title := item.Title
		desc := ""
		if item.Description != "" {
			desc = " " + pickerDescStyle.Render(item.Description)
		}

		if len(visible[i].matchIdxs) > 0 {
			src := item.FilterValue
			idxs := visible[i].matchIdxs
			if item.FilterValue != item.Title {
				idxs = filterMatchesForString(item.Title, idxs, item.FilterValue)
				src = item.Title
			}
			title = highlightMatches(src, idxs, nil)
		}

		icon := RenderActiveIndicator(item.Active)

		line := " " + icon + " " + title + desc

		if isSelected {
			lineW := lipgloss.Width(line)
			if lineW < m.width {
				line += strings.Repeat(" ", m.width-lineW)
			}
			line = pickerSelectedStyle.Render(line)
		}

		b.WriteString(line + "\n")
	}

	// Input + help at the bottom.
	b.WriteString("  " + m.input.View() + "\n")
	count := pickerCountStyle.Render(fmt.Sprintf("  %d/%d", len(visible), len(m.items)))
	var help string
	if m.confirm != "" {
		help = pickerConfirmStyle.Render("  " + m.confirm)
	} else if m.mode == pickerInsert {
		help = pickerCountStyle.Render("  esc: select mode  enter: switch  ctrl+c: quit")
	} else {
		help = pickerCountStyle.Render("  j/k: navigate  i,/: filter  x: close  enter: switch  esc,q: back")
	}
	b.WriteString(count + help + "\n")

	return b.String()
}

// highlightMatches renders a string with matched character positions in orange/bold.
// Non-matched characters use baseStyle (pass nil style for default).
func highlightMatches(s string, matchIdxs []int, baseStyle *lipgloss.Style) string {
	if len(matchIdxs) == 0 {
		if baseStyle != nil {
			return baseStyle.Render(s)
		}
		return s
	}
	matchSet := make(map[int]bool, len(matchIdxs))
	for _, idx := range matchIdxs {
		matchSet[idx] = true
	}

	var b strings.Builder
	for i, ch := range s {
		if matchSet[i] {
			b.WriteString(pickerMatchStyle.Render(string(ch)))
		} else if baseStyle != nil {
			b.WriteString(baseStyle.Render(string(ch)))
		} else {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// filterMatchesForString maps match indices from FilterValue to Title.
func filterMatchesForString(title string, matchIdxs []int, filterValue string) []int {
	// Only keep indices that fall within title length.
	var result []int
	for _, idx := range matchIdxs {
		if idx < len(title) {
			result = append(result, idx)
		}
	}
	return result
}
