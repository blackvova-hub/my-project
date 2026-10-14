package macroapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const sentimentSourceURL = "https://alternative.me/crypto/fear-and-greed-index/#api"

// Alternative.me explicitly permits commercial display when source attribution
// is immediately next to the indicator. Frontend preserves the clickable label.
// The source describes this index as Bitcoin-only; it is not market-wide mood.
func (a *API) refreshSentiment(ctx context.Context) {
	data, err := a.get(ctx, "https://api.alternative.me/fng/?limit=90&format=json")
	var item MarketItem
	if err == nil {
		item, err = parseSentiment(data, a.now())
	}
	a.mu.Lock()
	if err == nil {
		a.state.Sentiment = &item
	} else if a.state.Sentiment != nil {
		copy := *a.state.Sentiment
		copy.Status = "stale"
		a.state.Sentiment = &copy
	}
	a.mu.Unlock()
	a.health("alternative-sentiment", err)
}

func parseSentiment(data []byte, now time.Time) (MarketItem, error) {
	var raw struct {
		Data []struct {
			Value          string `json:"value"`
			Classification string `json:"value_classification"`
			Timestamp      string `json:"timestamp"`
		} `json:"data"`
		Metadata struct {
			Error json.RawMessage `json:"error"`
		} `json:"metadata"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return MarketItem{}, errors.New("Alternative.me вернул неверный JSON")
	}
	if len(raw.Metadata.Error) > 0 && string(raw.Metadata.Error) != "null" {
		return MarketItem{}, errors.New("Alternative.me сообщил об ошибке API")
	}
	points := []Point{}
	latest := time.Time{}
	latestValue := float64(0)
	classification := ""
	for _, p := range raw.Data {
		value, e1 := strconv.ParseFloat(p.Value, 64)
		ts, e2 := strconv.ParseInt(p.Timestamp, 10, 64)
		if e1 != nil || e2 != nil || !finite(value) || value < 0 || value > 100 || ts <= 0 {
			continue
		}
		at := time.Unix(ts, 0).UTC()
		if at.After(now.UTC().Add(5 * time.Minute)) {
			continue
		}
		points = append(points, Point{at.Format("2006-01-02"), value})
		if at.After(latest) {
			latest = at
			latestValue = value
			classification = strings.TrimSpace(p.Classification)
		}
	}
	points = normalizePoints(points)
	if len(points) == 0 {
		return MarketItem{}, errors.New("Alternative.me не вернул достоверных наблюдений 0–100")
	}
	if len(points) > 90 {
		points = points[len(points)-90:]
	}
	status := "fresh"
	if now.Sub(latest) > 48*time.Hour {
		status = "stale"
	}
	item := MarketItem{Symbol: "CRYPTO_FEAR_GREED", Name: "Bitcoin Fear & Greed", AssetClass: "sentiment", Price: ptr(latestValue), Unit: "баллы 0–100", Source: "Alternative.me", SourceURL: sentimentSourceURL, ObservedAt: ptr(latest.Format(time.RFC3339)), Points: points, Status: status, Note: "Дневной составной индекс настроений Bitcoin по методике Alternative.me: волатильность, объём/импульс, социальные сигналы, доминирование и тренды. Он не измеряет настроения всех финансовых рынков и не гарантирует будущую цену. Опросы приостановлены в методике источника. Шкала: 0 — крайний страх, 100 — крайняя жадность."}
	if classification != "" && len(classification) <= 100 {
		item.Classification = ptr(classification)
	}
	return item, nil
}
