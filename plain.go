package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/serge/cms/internal/config"
	"github.com/serge/cms/internal/tui"
	"github.com/serge/cms/internal/watcher"
)

// runPlainMode drives the finder headlessly and prints plain text to stdout.
// With watch=true it keeps running, re-rendering on watcher updates.
func runPlainMode(sections []string, cfg config.Config, watch bool) {
	w := watcher.New()
	w.ApplyConfig(cfg.General, cfg.Status)

	if !watch {
		w.BootstrapSync()
		h := tui.RunHeadless(cfg, w, sections)
		printPlain(h.PlainSnapshot(), sections)
		return
	}

	// Watch mode: start watcher with channel-based callback.
	ch := make(chan tea.Msg, 32)
	w.Start(func(msg tea.Msg) {
		select {
		case ch <- msg:
		default:
		}
	})
	defer w.Stop()

	// Wait for initial StateMsg so the finder has session data.
	var h *tui.HeadlessFinder
	for msg := range ch {
		if _, ok := msg.(watcher.StateMsg); ok {
			// Bootstrap done — create finder with initial state.
			h = tui.RunHeadless(cfg, w, sections)
			// Feed the StateMsg so the finder's internal state matches.
			h.UpdateFromWatcher(msg)
			break
		}
	}
	if h == nil {
		// No tmux — still render what we have (projects, etc.)
		h = tui.RunHeadless(cfg, w, sections)
	}

	printPlain(h.PlainSnapshot(), sections)

	// Watch loop: debounce + re-render on updates.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	debounce := time.NewTimer(time.Hour) // dormant until first update
	debounce.Stop()
	dirty := false

	for {
		select {
		case <-sig:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if h.UpdateFromWatcher(msg) {
				dirty = true
				debounce.Reset(100 * time.Millisecond)
			}
		case <-debounce.C:
			if dirty {
				fmt.Print("\033[2J\033[H") // clear screen + cursor home
				printPlain(h.PlainSnapshot(), sections)
				dirty = false
			}
		}
	}
}

func printPlain(rows []tui.PlainRow, sections []string) {
	if len(rows) == 0 {
		fmt.Println("(no items)")
		return
	}

	currentSection := ""
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		if row.Section != currentSection {
			if currentSection != "" {
				tw.Flush()
				fmt.Println()
			}
			currentSection = row.Section
			// Count items in this section.
			count := 0
			for _, r := range rows {
				if r.Section == currentSection {
					count++
				}
			}
			fmt.Printf("## %s (%d)\n", currentSection, count)
		}
		marker := "  "
		if row.Active {
			marker = "* "
		}
		desc := strings.TrimRight(row.Desc, " ")
		fmt.Fprintf(tw, "%s%s\t%s\n", marker, row.Title, desc)
	}
	tw.Flush()
}
