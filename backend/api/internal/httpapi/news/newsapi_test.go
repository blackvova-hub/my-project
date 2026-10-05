package newsapi

import (
	"os"
	"strings"
	"testing"
)

func TestNewsListWithoutDateDoesNotExcludeCalendarOnlyItems(t *testing.T) {
	source, err := os.ReadFile("newsapi.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(source)), "and calendar_only = false") {
		t.Fatal("news list must not hide calendar-only items when date is omitted")
	}
}

func TestPublicNewsQueriesRequirePublicationEligibility(t *testing.T) {
	source, err := os.ReadFile("newsapi.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.ToLower(string(source)), "publication_eligible = true") < 2 {
		t.Fatal("both public news list and calendar queries must require publication eligibility")
	}
}
