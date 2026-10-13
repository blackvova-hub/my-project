package chartconfigapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type OrderflowCell struct {
	Time      int64   `json:"time"`
	PriceLow  float64 `json:"price_low"`
	PriceHigh float64 `json:"price_high"`
	Value     float64 `json:"value"`
	Buy       float64 `json:"buy,omitempty"`
	Sell      float64 `json:"sell,omitempty"`
}
type OrderflowTrade struct {
	ID       string  `json:"id"`
	Time     int64   `json:"time"`
	Price    float64 `json:"price"`
	Quantity float64 `json:"quantity"`
	Side     string  `json:"side"`
	Exchange string  `json:"exchange"`
}
type OrderflowDataset struct {
	Layer     string           `json:"layer"`
	Symbol    string           `json:"symbol"`
	Exchange  string           `json:"exchange"`
	Market    string           `json:"market"`
	Unit      string           `json:"unit"`
	Source    string           `json:"source"`
	Coverage  []string         `json:"coverage"`
	UpdatedAt int64            `json:"updated_at"`
	Estimated bool             `json:"estimated"`
	Cells     []OrderflowCell  `json:"cells"`
	Trades    []OrderflowTrade `json:"trades"`
}
type OrderflowQuery struct {
	Layer, Symbol, Exchange, Market, Interval string
	From, To                                  int64
}
type OrderflowProvider interface {
	Fetch(context.Context, OrderflowQuery) (OrderflowDataset, error)
	Supports(string) bool
}
type HTTPOrderflowProvider struct {
	BaseURL, Token string
	Layers         map[string]bool
	Client         *http.Client
}

var orderflowRequirements = map[string]string{
	"liquidation-heatmap": "Требуется источник оценочных уровней ликвидации по цене и времени",
	"orderbook-heatmap":   "Требуется история глубины стакана по цене и времени",
	"footprint":           "Требуются исполнения buy/sell на каждом ценовом уровне свечи",
	"large-trades":        "Требуются отдельные исполнения с временем, ценой, размером и стороной",
}
var orderflowSymbol = regexp.MustCompile(`^[A-Z0-9]{2,40}$`)

func orderflowFromEnvironment() OrderflowProvider {
	base, token := strings.TrimRight(os.Getenv("CHART_ORDERFLOW_PROVIDER_URL"), "/"), os.Getenv("CHART_ORDERFLOW_PROVIDER_TOKEN")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || token == "" {
		return nil
	}
	layers := map[string]bool{}
	for _, layer := range strings.Split(os.Getenv("CHART_ORDERFLOW_LAYERS"), ",") {
		layer = strings.TrimSpace(layer)
		if _, ok := orderflowRequirements[layer]; ok {
			layers[layer] = true
		}
	}
	return &HTTPOrderflowProvider{BaseURL: base, Token: token, Layers: layers, Client: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (p *HTTPOrderflowProvider) Supports(layer string) bool { return p.Layers[layer] }
func (p *HTTPOrderflowProvider) Fetch(ctx context.Context, q OrderflowQuery) (OrderflowDataset, error) {
	var result OrderflowDataset
	if !p.Supports(q.Layer) {
		return result, errors.New("unsupported_layer")
	}
	params := url.Values{"symbol": {q.Symbol}, "exchange": {q.Exchange}, "market": {q.Market}, "interval": {q.Interval}, "from": {strconv.FormatInt(q.From, 10)}, "to": {strconv.FormatInt(q.To, 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+"/v1/charts/"+q.Layer+"?"+params.Encode(), nil)
	if err != nil {
		return result, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	req.Header.Set("Accept", "application/json")
	response, err := p.Client.Do(req)
	if err != nil {
		return result, errors.New("provider_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return result, errors.New("provider_rejected_request")
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if decoder.Decode(&result) != nil {
		return result, errors.New("invalid_provider_response")
	}
	if err := validateOrderflow(result, q); err != nil {
		return OrderflowDataset{}, err
	}
	return result, nil
}
func validateOrderflow(d OrderflowDataset, q OrderflowQuery) error {
	finite := func(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }
	if d.Layer != q.Layer || d.Symbol != q.Symbol || d.Exchange != q.Exchange || d.Market != q.Market || d.Source == "" || len(d.Source) > 300 || len(d.Coverage) == 0 || len(d.Coverage) > 30 || d.Unit == "" || len(d.Unit) > 40 || d.UpdatedAt <= 0 || len(d.Cells) > 50000 || len(d.Trades) > 10000 {
		return errors.New("invalid_provider_metadata")
	}
	if q.Layer == "liquidation-heatmap" && !d.Estimated {
		return errors.New("liquidation_heatmap_must_be_estimated")
	}
	for _, c := range d.Cells {
		if c.Time < q.From || c.Time > q.To || !finite(c.PriceLow) || !finite(c.PriceHigh) || !finite(c.Value) || !finite(c.Buy) || !finite(c.Sell) || c.PriceLow <= 0 || c.PriceHigh < c.PriceLow || c.Value < 0 || c.Buy < 0 || c.Sell < 0 {
			return errors.New("invalid_provider_cell")
		}
	}
	seen := map[string]bool{}
	for _, t := range d.Trades {
		if t.ID == "" || len(t.ID) > 100 || seen[t.ID] || t.Time < q.From || t.Time > q.To || !finite(t.Price) || !finite(t.Quantity) || t.Price <= 0 || t.Quantity <= 0 || (t.Side != "buy" && t.Side != "sell") || t.Exchange == "" {
			return errors.New("invalid_provider_trade")
		}
		seen[t.ID] = true
	}
	return nil
}
func (a *API) handleOrderflowCapabilities(w http.ResponseWriter, r *http.Request) {
	layers := []map[string]any{}
	for _, id := range []string{"liquidation-heatmap", "orderbook-heatmap", "footprint", "large-trades"} {
		layers = append(layers, map[string]any{"id": id, "available": a.orderflow != nil && a.orderflow.Supports(id), "requires": orderflowRequirements[id]})
	}
	writeJSON(w, 200, map[string]any{"layers": layers, "contract": "shortlong-orderflow-v1", "configured": a.orderflow != nil})
}
func (a *API) handleOrderflow(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	from, _ := strconv.ParseInt(values.Get("from"), 10, 64)
	to, _ := strconv.ParseInt(values.Get("to"), 10, 64)
	q := OrderflowQuery{Layer: chi.URLParam(r, "layer"), Symbol: values.Get("symbol"), Exchange: values.Get("exchange"), Market: values.Get("market"), Interval: values.Get("interval"), From: from, To: to}
	_, known := orderflowRequirements[q.Layer]
	_, interval := allowedIntervals[q.Interval]
	if !known || !interval || !orderflowSymbol.MatchString(q.Symbol) || (q.Exchange != "bybit" && q.Exchange != "binance") || (q.Market != "linear" && q.Market != "spot") || from <= 0 || to <= from || to-from > 90*86400 || to > time.Now().Unix()+300 {
		writeErr(w, 400, "invalid_orderflow_query")
		return
	}
	if a.orderflow == nil || !a.orderflow.Supports(q.Layer) {
		writeJSON(w, 503, map[string]string{"error": "orderflow_source_not_configured", "requires": orderflowRequirements[q.Layer]})
		return
	}
	result, err := a.orderflow.Fetch(r.Context(), q)
	if err != nil {
		writeErr(w, 502, "orderflow_source_unavailable")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, 200, result)
}
