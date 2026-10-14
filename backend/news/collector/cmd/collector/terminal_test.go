package main

import (
	"context"
	"testing"
)

func TestTerminalSnapshotCannotApprovePublication(t *testing.T) {
	// No SQL must run for an item the normal collector pipeline did not approve.
	// Passing nil makes an accidental write fail immediately.
	claimed, err := claimTerminalNewsItem(context.Background(), nil, NewsItem{PublicationEligible: false})
	if claimed || err != nil {
		t.Fatalf("unmoderated terminal snapshot was approved: claimed=%v err=%v", claimed, err)
	}
}
