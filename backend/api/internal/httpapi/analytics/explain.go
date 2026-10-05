package analytics

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func (a *API) explain(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Type string   `json:"type"`
		IDs  []string `json:"ids"`
	}
	if !decode(w, r, &input) {
		return
	}
	allowed := map[string]bool{"weak-symbol": true, "strong-symbol": true, "weak-time": true, "leverage": true, "exit": true, "funding": true, "cost-spike": true, "drawdown": true, "concentration": true, "behavior": true}
	if !allowed[input.Type] || len(input.IDs) == 0 || len(input.IDs) > 3000 {
		fail(w, 400, "Select a supported pattern with affected trades.")
		return
	}
	data, err := a.load(r.Context(), owner(r))
	if err != nil {
		fail(w, 500, "Unable to load calculation evidence.")
		return
	}
	ids := map[string]bool{}
	for _, id := range input.IDs {
		ids[id] = true
	}
	count := 0
	net, gross, fees, funding, winning, losing := 0.0, 0.0, 0.0, 0.0, 0.0, 0.0
	symbols := map[string]int{}
	for _, t := range data.Trades {
		if !ids[t.ID] || !t.Complete || t.ClosedAt == nil {
			continue
		}
		count++
		net += t.Net
		gross += t.Gross
		fees += t.Fees
		funding += t.Funding
		symbols[t.Symbol]++
		if t.Net > 0 {
			winning += t.Net
		} else {
			losing -= t.Net
		}
	}
	if count == 0 {
		fail(w, 404, "No complete owned trades match this pattern.")
		return
	}
	var pf *float64
	if losing > 0 {
		v := winning / losing
		pf = &v
	}
	evidence := map[string]any{"pattern": input.Type, "sample": count, "netPnl": net, "grossPnl": gross, "fees": fees, "funding": funding, "profitFactor": pf, "symbols": symbols}
	fallback := fmt.Sprintf("Сделок с полной историей в выбранной группе: %d. Чистый результат: %.2f $, торговые комиссии: %.2f $, итог финансирования: %.2f $. Эти данные описывают прошедшие сделки, но не устанавливают причины и не предсказывают будущую доходность.", count, net, fees, funding)
	key := os.Getenv("OPENAI_API_KEY")
	model := os.Getenv("OPENAI_MODEL")
	if key == "" || model == "" {
		respond(w, 200, map[string]any{"text": fallback, "source": "deterministic", "evidence": evidence})
		return
	}
	encoded, _ := json.Marshal(evidence)
	// Include the language in the cache key so older English explanations are not reused.
	hash := sha256.Sum256(append([]byte("ru-RU:v1:"), encoded...))
	hashString := hex.EncodeToString(hash[:])
	var cached string
	if a.db.QueryRow(r.Context(), `SELECT explanation FROM analytics_explanations WHERE user_id=$1 AND evidence_hash=$2 AND explanation<>''`, owner(r), hashString).Scan(&cached) == nil {
		respond(w, 200, map[string]any{"text": cached, "source": "AI · cached", "evidence": evidence})
		return
	}
	// Reserve under an owner advisory lock so concurrent requests cannot exceed
	// the paid-request budget, or duplicate a request for the same evidence.
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		fail(w, 500, "Unable to reserve explanation.")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT pg_advisory_xact_lock(hashtext($1))`, owner(r)); err != nil {
		fail(w, 500, "Unable to reserve explanation.")
		return
	}
	var used int
	if err = tx.QueryRow(r.Context(), `SELECT count(*) FROM analytics_explanations WHERE user_id=$1 AND created_at>now()-interval '1 day'`, owner(r)).Scan(&used); err != nil {
		fail(w, 500, "Unable to check explanation budget.")
		return
	}
	if used >= 20 {
		respond(w, 200, map[string]any{"text": fallback, "source": "deterministic · daily AI limit", "evidence": evidence})
		return
	}
	tag, err := tx.Exec(r.Context(), `INSERT INTO analytics_explanations(user_id,evidence_hash) VALUES($1,$2) ON CONFLICT DO NOTHING`, owner(r), hashString)
	if err != nil || tag.RowsAffected() == 0 {
		respond(w, 200, map[string]any{"text": fallback, "source": "deterministic", "evidence": evidence})
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail(w, 500, "Unable to reserve explanation.")
		return
	}
	instructions := "Explain this server-calculated trading segment in two short factual sentences in Russian. Use clear Russian terms, not English financial jargon. Do not calculate, estimate, infer causality, recommend trades, or predict. Do not claim significance. The pattern label is a hypothesis; only describe the supplied aggregate evidence. Write no numbers, percentages or new metrics: exact values are displayed separately by the application. Do not mention individual identity."
	payload, _ := json.Marshal(map[string]any{"model": model, "instructions": instructions, "input": string(encoded), "max_output_tokens": 500, "reasoning": map[string]string{"effort": "low"}, "text": map[string]string{"verbosity": "low"}, "store": false})
	base := strings.TrimRight(os.Getenv("OPENAI_BASE_URL"), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	request, err := http.NewRequestWithContext(r.Context(), "POST", base+"/responses", bytes.NewReader(payload))
	if err != nil {
		respond(w, 200, map[string]any{"text": fallback, "source": "deterministic", "evidence": evidence})
		return
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 18 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	text := ""
	if err == nil {
		defer response.Body.Close()
		if response.StatusCode == 200 {
			var result struct {
				Output []struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			}
			if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) == nil {
				for _, item := range result.Output {
					for _, part := range item.Content {
						if part.Type == "output_text" {
							text += part.Text
						}
					}
				}
			}
		}
	}
	source := "AI explanation"
	if strings.TrimSpace(text) == "" || len(text) > 3000 || strings.ContainsAny(text, "0123456789") {
		text = fallback
		source = "deterministic · AI unavailable"
	} else {
		_, _ = a.db.Exec(r.Context(), `UPDATE analytics_explanations SET explanation=$3 WHERE user_id=$1 AND evidence_hash=$2`, owner(r), hashString, text)
	}
	respond(w, 200, map[string]any{"text": text, "source": source, "evidence": evidence})
}
