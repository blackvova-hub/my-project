package newsapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTerminalImportanceRecognizesMajorReleasesWithoutBlanketSourceBoost(t *testing.T) {
	cases := []struct {
		name, title, source, url, category, want string
		official                                 bool
	}{
		{"FOMC full name", "Minutes of the Federal Open Market Committee, September 15-16, 2026", "federal_reserve", "https://www.federalreserve.gov/newsevents/pressreleases/monetary20261007a.htm", "macro", "high", true},
		{"full FOMC name independent of URL", "Federal Open Market Committee announces decision", "federal_reserve", "https://www.federalreserve.gov/newsevents/1", "macro", "high", true},
		{"discount rate board minutes", "Minutes of the Board's discount rate meetings", "federal_reserve", "https://www.federalreserve.gov/newsevents/1", "macro", "high", true},
		{"ECB policy account", "Meeting of 9-10 September 2026", "ecb", "https://www.ecb.europa.eu/press/accounts/2026/html/ecb.mg261014.en.html", "macro", "high", true},
		{"ECB unrelated meeting", "Meeting of 9-10 September 2026", "ecb", "https://www.ecb.europa.eu/press/pr/date/2026/html/1.html", "macro", "low", true},
		{"spoofed ECB URL", "Meeting of 9-10 September 2026", "ecb", "https://ecb.europa.eu.example.org/press/accounts/2026/1", "macro", "low", true},
		{"Fed neutral appointment", "Federal Reserve Board appoints a new director", "federal_reserve", "https://www.federalreserve.gov/newsevents/pressreleases/other20261014a.htm", "macro", "low", true},
		{"Fed enforcement", "Federal Reserve Board issues enforcement action against a former employee", "federal_reserve", "https://www.federalreserve.gov/newsevents/pressreleases/enforcement20261014a.htm", "macro", "medium", true},
		{"neutral publisher", "Publisher announces new newsroom design", "gdelt", "https://example.org/1", "world", "low", false},
		{"CPI acronym", "CPI-U release for September", "bls", "https://www.bls.gov/news.release/cpi.htm", "macro", "high", true},
		{"PCE acronym", "Core PCE price index released", "bls", "https://www.bls.gov/news.release/1", "macro", "high", true},
		{"NFP abbreviation", "September NFP report published", "bls", "https://www.bls.gov/news.release/empsit.htm", "macro", "high", true},
		{"GDP acronym", "Third-quarter GDP report", "bls", "https://www.bls.gov/news.release/1", "macro", "high", true},
		{"GDP token boundary", "GDPR privacy guidance published", "ecb", "https://www.ecb.europa.eu/press/pr/1", "macro", "low", true},
		{"Russian CPI", "Базовый ИПЦ: публикация данных", "bls", "https://www.bls.gov/news.release/1", "macro", "high", true},
		{"Russian FOMC", "Заседание Федерального комитета по операциям на открытом рынке", "federal_reserve", "https://www.federalreserve.gov/newsevents/1", "macro", "high", true},
		{"mainnet upgrade", "Ethereum mainnet upgrade announcement", "ethereum_foundation", "https://blog.ethereum.org/2026/10/14/mainnet", "crypto", "medium", true},
		{"protocol security", "Critical security update: client vulnerability", "ethereum_foundation", "https://blog.ethereum.org/2026/10/14/security", "crypto", "medium", true},
		{"generic security research", "Native transaction assertions and wallet security research", "ethereum_foundation", "https://blog.ethereum.org/2026/10/14/research", "crypto", "low", true},
		{"testnet", "Glamsterdam Testnet Announcement", "ethereum_foundation", "https://blog.ethereum.org/2026/10/14/testnet", "crypto", "low", true},
		{"default is not a sovereign default", "Client default configuration updated", "ethereum_foundation", "https://blog.ethereum.org/2026/10/14/config", "crypto", "low", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := TerminalNewsItem{Title: tc.title, SourceID: tc.source, URL: tc.url, Category: tc.category, PublishedAt: terminalTestNow.Add(-time.Hour).Format(time.RFC3339), TimestampBasis: "published"}
			if tc.name == "testnet" {
				item.Summary = "This network upgrade tests features before the future mainnet upgrade. Mainnet activation dates have not been decided."
			}
			enrichTerminalItem(&item, tc.official, terminalTestNow)
			if item.Importance != tc.want {
				t.Fatalf("importance=%s score=%d reasons=%v; want %s", item.Importance, item.ImportanceScore, item.ImportanceReason, tc.want)
			}
			if item.Breaking {
				t.Fatal("importance ranking fabricated a Breaking label")
			}
			if tc.name == "ECB policy account" && !strings.Contains(strings.Join(item.ImportanceReason, " "), "раздел первоисточника") {
				t.Fatal("policy-account provenance criterion not explained")
			}
		})
	}
}

func TestTerminalBreakingRequiresActualRecentPublishedPrefix(t *testing.T) {
	cases := []struct {
		name, title, basis string
		age                time.Duration
		want               bool
	}{
		{"fresh", "BREAKING: Federal funds rate decision", "published", 9 * time.Minute, true},
		{"boundary", "СРОЧНО: Решение по процентной ставке", "published", 10 * time.Minute, true},
		{"expired", "Breaking news: Federal funds rate decision", "published", 11 * time.Minute, false},
		{"indexed", "BREAKING: Old report recently indexed", "indexed", time.Minute, false},
		{"future", "BREAKING: Scheduled release", "published", -time.Minute, false},
		{"phrase in commentary", "What breaking: headlines mean for markets", "published", time.Minute, false},
		{"important alone", "Federal Open Market Committee decision", "published", time.Minute, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			item := TerminalNewsItem{Title: tc.title, SourceID: "federal_reserve", Category: "macro", PublishedAt: terminalTestNow.Add(-tc.age).Format(time.RFC3339), TimestampBasis: tc.basis}
			enrichTerminalItem(&item, true, terminalTestNow)
			if item.Breaking != tc.want {
				t.Fatalf("breaking=%v want %v", item.Breaking, tc.want)
			}
		})
	}
}

func TestTerminalWarmCacheReappliesCurrentImportanceRules(t *testing.T) {
	s := newTerminalService(nil)
	s.now = func() time.Time { return terminalTestNow }
	s.syncing = true
	s.items = []TerminalNewsItem{{ID: "cached", Title: "Minutes of the Federal Open Market Committee, September 15-16, 2026", SourceID: "federal_reserve", Category: "macro", URL: "https://www.federalreserve.gov/newsevents/release", PublishedAt: terminalTestNow.Add(-time.Hour).Format(time.RFC3339), TimestampBasis: "published", Importance: "low", ImportanceScore: 15, Assets: []string{"BTC"}}}
	w := httptest.NewRecorder()
	s.handleList(w, httptest.NewRequest(http.MethodGet, "/news/terminal?importance=high", nil))
	var response struct {
		Items []TerminalNewsItem `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(response.Items) != 1 || response.Items[0].Importance != "high" || !terminalMember("BTC", response.Items[0].Assets...) {
		t.Fatalf("cached old ranking or asset metadata was not refreshed: %+v", response.Items)
	}
}
