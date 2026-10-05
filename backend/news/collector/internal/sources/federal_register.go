package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type FederalRegister struct {
	URL    string
	Limit  int
	Client HTTPClient
}

func (s *FederalRegister) ID() string { return "federal_register" }

type federalRegisterList struct {
	Results []struct {
		Title          string `json:"title"`
		Abstract       string `json:"abstract"`
		DocumentNumber string `json:"document_number"`
		HTMLURL        string `json:"html_url"`
		JSONURL        string `json:"json_url"`
		Publication    string `json:"publication_date"`
	} `json:"results"`
}

type federalRegisterDetail struct {
	Title          string `json:"title"`
	Abstract       string `json:"abstract"`
	DocumentNumber string `json:"document_number"`
	HTMLURL        string `json:"html_url"`
	RawTextURL     string `json:"raw_text_url"`
	Subtype        string `json:"subtype"`
	Publication    string `json:"publication_date"`
	SigningDate    string `json:"signing_date"`
}

func (s *FederalRegister) Fetch(ctx context.Context) ([]Item, error) {
	rawURL := strings.TrimSpace(s.URL)
	if rawURL == "" {
		rawURL = "https://www.federalregister.gov/api/v1/documents.json?per_page=20&order=newest&conditions%5Bpresidential_document_type%5D%5B%5D=executive_order"
	}
	body, _, err := Get(ctx, s.Client, rawURL, "application/json", 4<<20)
	if err != nil {
		return nil, err
	}
	var list federalRegisterList
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("decode Federal Register list: %w", err)
	}
	limit := s.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	out := make([]Item, 0, limit)
	errors := make([]string, 0)
	for _, row := range list.Results {
		if len(out) >= limit {
			break
		}
		detailURL := strings.TrimSpace(row.JSONURL)
		if detailURL == "" && row.DocumentNumber != "" {
			detailURL = "https://www.federalregister.gov/api/v1/documents/" + row.DocumentNumber + ".json"
		}
		detailBody, _, fetchErr := Get(ctx, s.Client, detailURL, "application/json", 4<<20)
		if fetchErr != nil {
			errors = append(errors, row.DocumentNumber+": "+fetchErr.Error())
			continue
		}
		var detail federalRegisterDetail
		if err := json.Unmarshal(detailBody, &detail); err != nil {
			errors = append(errors, row.DocumentNumber+": "+err.Error())
			continue
		}
		if !isMarketPresidentialDocument(detail.Subtype) {
			continue
		}
		text := strings.TrimSpace(first(detail.Abstract, row.Abstract))
		if detail.RawTextURL != "" {
			if raw, _, textErr := Get(ctx, s.Client, detail.RawTextURL, "text/plain", 3<<20); textErr == nil {
				text = limitText(strings.TrimSpace(string(raw)), 16_000)
			} else {
				errors = append(errors, row.DocumentNumber+" text: "+textErr.Error())
			}
		}
		title := strings.TrimSpace(first(detail.Title, row.Title))
		if !officialGovernmentMarketRelevant(title + " " + text) {
			continue
		}
		published, err := time.Parse("2006-01-02", first(detail.Publication, row.Publication))
		if err != nil {
			errors = append(errors, row.DocumentNumber+": invalid publication date")
			continue
		}
		var eventAt *time.Time
		if signed, err := time.Parse("2006-01-02", detail.SigningDate); err == nil {
			eventAt = TimePtr(signed.UTC())
		}
		out = append(out, Item{
			Category: "macro", Title: title, URL: first(detail.HTMLURL, row.HTMLURL),
			Source: "Federal Register", SourceID: s.ID(), SourceType: "official_government",
			PublishedAt: published.UTC(), EventAt: eventAt, EventType: "major_statement",
			Summary: text, OriginalLanguage: "en",
			Entities: []string{"Executive Office of the President"},
		})
	}
	return out, partialError(1, errors)
}

func isMarketPresidentialDocument(subtype string) bool {
	switch strings.ToLower(strings.TrimSpace(subtype)) {
	case "executive order", "presidential memorandum", "presidential determination":
		return true
	default:
		return false
	}
}

func officialGovernmentMarketRelevant(text string) bool {
	text = strings.ToLower(text)
	terms := []string{
		"crypto", "cryptocurrency", "digital asset", "bitcoin", "ethereum", "stablecoin", "blockchain",
		"tokenization", "decentralized finance", "strategic reserve", "exchange-traded fund", " etf",
		"securities", "broker-dealer", "financial market", "capital market", "banking restriction",
		"bank regulation", "interest rate", "monetary policy", "federal reserve", "inflation",
		"treasury market", "financial stability", "systemic risk", "sanction", "ofac",
		"tariff", "trade restriction", "trade war", "import duty", "export control", "capital control",
		"debt ceiling", "government shutdown", "currency intervention", "dollar liquidity",
	}
	return containsAny(text, terms...)
}

func limitText(value string, limit int) string {
	runes := []rune(value)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit])
	}
	return value
}

var _ Source = (*FederalRegister)(nil)
