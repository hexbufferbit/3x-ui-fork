package service

import (
	"testing"
	"time"
)

func TestPruneActiveClientIPsRemovesOfflineStaleIPs(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	entries := []StoredClientIP{
		{IP: "1.1.1.1", Timestamp: now.Add(-2 * time.Minute).Unix()},
		{IP: "2.2.2.2", Timestamp: now.Add(-30 * time.Second).Unix()},
	}

	got := PruneActiveClientIPs(entries, false, now)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].IP != "2.2.2.2" {
		t.Fatalf("got[0].IP = %q, want %q", got[0].IP, "2.2.2.2")
	}
}

func TestPruneActiveClientIPsKeepsLatestOnlineIPWhenNoFreshEntries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	entries := []StoredClientIP{
		{IP: "1.1.1.1", Timestamp: now.Add(-2 * time.Minute).Unix()},
		{IP: "2.2.2.2", Timestamp: now.Add(-3 * time.Minute).Unix()},
	}

	got := PruneActiveClientIPs(entries, true, now)
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].IP != "1.1.1.1" {
		t.Fatalf("got[0].IP = %q, want %q", got[0].IP, "1.1.1.1")
	}
}
