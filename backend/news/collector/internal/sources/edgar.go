package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type EDGAR struct {
	CIKs   []string
	Client HTTPClient
	MaxAge time.Duration
	Now    func() time.Time
}

func (s *EDGAR) ID() string { return "sec_edgar" }

type edgarSubmission struct {
	Name    string `json:"name"`
	Filings struct {
		Recent struct {
			AccessionNumber []string `json:"accessionNumber"`
			FilingDate      []string `json:"filingDate"`
			Acceptance      []string `json:"acceptanceDateTime"`
			ReportDate      []string `json:"reportDate"`
			Form            []string `json:"form"`
			PrimaryDocument []string `json:"primaryDocument"`
			PrimaryDocDesc  []string `json:"primaryDocDescription"`
		} `json:"recent"`
	} `json:"filings"`
}

var cikDigits = regexp.MustCompile(`\D`)

func (s *EDGAR) Fetch(ctx context.Context) ([]Item, error) {
	var out []Item
	var errs []string
	successes := 0
	maxAge := s.MaxAge
	if maxAge <= 0 {
		maxAge = 14 * 24 * time.Hour
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	for _, rawCIK := range s.CIKs {
		cik := cikDigits.ReplaceAllString(rawCIK, "")
		if cik == "" {
			continue
		}
		cik = fmt.Sprintf("%010s", cik)
		body, _, err := Get(ctx, s.Client, "https://data.sec.gov/submissions/CIK"+cik+".json", "application/json", 8<<20)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		var data edgarSubmission
		if err := json.Unmarshal(body, &data); err != nil {
			errs = append(errs, fmt.Sprintf("decode CIK %s: %v", cik, err))
			continue
		}
		successes++
		rows := data.Filings.Recent
		for i, form := range rows.Form {
			form = strings.ToUpper(strings.TrimSpace(form))
			if !allowedForm(form) || i >= len(rows.AccessionNumber) || i >= len(rows.FilingDate) || i >= len(rows.PrimaryDocument) {
				continue
			}
			published, err := time.Parse("2006-01-02", rows.FilingDate[i])
			fallback := true
			if i < len(rows.Acceptance) && strings.TrimSpace(rows.Acceptance[i]) != "" {
				for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "20060102150405"} {
					if accepted, parseErr := time.Parse(layout, strings.TrimSpace(rows.Acceptance[i])); parseErr == nil {
						published, fallback = accepted, false
						break
					}
				}
			}
			if err != nil && published.IsZero() {
				continue
			}
			if published.Before(now.Add(-maxAge)) || published.After(now.Add(15*time.Minute)) {
				continue
			}
			accession := strings.ReplaceAll(rows.AccessionNumber[i], "-", "")
			cikNoZero := strings.TrimLeft(cik, "0")
			link := fmt.Sprintf("https://www.sec.gov/Archives/edgar/data/%s/%s/%s", cikNoZero, accession, url.PathEscape(rows.PrimaryDocument[i]))
			description := ""
			if i < len(rows.PrimaryDocDesc) {
				description = strings.TrimSpace(rows.PrimaryDocDesc[i])
			}
			title := fmt.Sprintf("%s filed Form %s", data.Name, form)
			if description != "" {
				title += ": " + description
			}
			out = append(out, Item{Category: edgarCategory(data.Name, description), Title: title, URL: link, Source: "SEC EDGAR", SourceID: s.ID(), SourceType: "official_government", PublishedAt: published.UTC(), PublishedFallback: fallback, EventType: edgarEventType(description), Summary: fmt.Sprintf("%s submitted Form %s to the SEC.", data.Name, form), OriginalLanguage: "en", Entities: []string{data.Name, "U.S. Securities and Exchange Commission"}})
		}
	}
	return out, partialError(successes, errs)
}

func allowedForm(form string) bool {
	for _, allowed := range []string{"8-K", "8-K/A", "S-1", "S-1/A", "10-Q", "10-Q/A", "13F-HR", "13F-HR/A"} {
		if form == allowed {
			return true
		}
	}
	return false
}

func edgarCategory(name, description string) string {
	if secRelevant(name, description) {
		return "crypto"
	}
	return "macro"
}

func edgarEventType(description string) string {
	if strings.Contains(strings.ToLower(description), "etf") {
		return "etf"
	}
	return "filing"
}

var _ Source = (*EDGAR)(nil)
