package main

import (
	"testing"

	"github.com/serge/cms/internal/agent"
)

func TestSelectNextPaneMixedProviders(t *testing.T) {
	all := []jumpCandidate{
		{paneID: "%1", activity: agent.ActivityWorking},
		{paneID: "%2", activity: agent.ActivityIdle},
		{paneID: "%3", activity: agent.ActivityWaitingInput},
	}

	if got := selectNextPane(all, 0); got != "%3" {
		t.Fatalf("selectNextPane = %q, want %%3", got)
	}
}
