package similarity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Candidate struct {
	ID      string    `json:"id"`
	Vector  []float64 `json:"vector"`
	Payload Payload   `json:"payload"`
}
type Payload struct {
	Market     string `json:"market"`
	Symbol     string `json:"symbol"`
	End        int64  `json:"end"`
	Trend      string `json:"trend"`
	Volatility string `json:"volatility"`
	Volume     string `json:"volume"`
}
type VectorIndex interface {
	Upsert(context.Context, int, []Candidate) error
	Search(context.Context, int, []float64, string, []string, int64, int) ([]Candidate, error)
}
type Qdrant struct {
	URL, Key, Version string
	HTTP              *http.Client
}

func NewQdrant(url, key, version string) *Qdrant {
	return &Qdrant{strings.TrimRight(url, "/"), key, version, &http.Client{Timeout: 45 * time.Second}}
}
func (q *Qdrant) path(n int) string {
	return fmt.Sprintf("/collections/similarity_%s_%d", q.Version, n)
}
func (q *Qdrant) call(ctx context.Context, method, path string, input, out any) error {
	var body []byte
	var e error
	if input != nil {
		body, e = json.Marshal(input)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, q.URL+path, bytes.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	if q.Key != "" {
		req.Header.Set("api-key", q.Key)
	}
	r, e := q.HTTP.Do(req)
	if e != nil {
		return e
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if e != nil {
		return e
	}
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return fmt.Errorf("qdrant HTTP %d: %.300s", r.StatusCode, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
func (q *Qdrant) Init(ctx context.Context, windows []Window) error {
	for _, w := range windows {
		var exists struct {
			Result struct {
				Exists bool `json:"exists"`
			} `json:"result"`
		}
		if e := q.call(ctx, "GET", q.path(w.Bars)+"/exists", nil, &exists); e != nil {
			return e
		}
		if !exists.Result.Exists {
			e := q.call(ctx, "PUT", q.path(w.Bars), map[string]any{"vectors": map[string]any{"size": Dimensions, "distance": "Euclid", "on_disk": true}, "on_disk_payload": true, "hnsw_config": map[string]any{"m": 16, "ef_construct": 100, "on_disk": true}, "optimizers_config": map[string]any{"indexing_threshold": 10000, "max_optimization_threads": 1}}, nil)
			if e != nil {
				return e
			}
		}
		for key, kind := range map[string]string{"market": "keyword", "symbol": "keyword", "end": "integer"} {
			if e := q.call(ctx, "PUT", q.path(w.Bars)+"/index?wait=true", map[string]any{"field_name": key, "field_schema": kind}, nil); e != nil {
				return e
			}
		}
	}
	return nil
}
func (q *Qdrant) Upsert(ctx context.Context, n int, points []Candidate) error {
	if len(points) == 0 {
		return nil
	}
	return q.call(ctx, "PUT", q.path(n)+"/points?wait=true", map[string]any{"points": points}, nil)
}
func (q *Qdrant) Search(ctx context.Context, n int, v []float64, market string, symbols []string, before int64, limit int) ([]Candidate, error) {
	if len(symbols) == 0 {
		return []Candidate{}, nil
	}
	filter := map[string]any{"must": []any{map[string]any{"key": "market", "match": map[string]any{"value": market}}, map[string]any{"key": "symbol", "match": map[string]any{"any": symbols}}, map[string]any{"key": "end", "range": map[string]any{"lte": before}}}}
	var out struct {
		Result struct {
			Points []Candidate `json:"points"`
		} `json:"result"`
	}
	err := q.call(ctx, "POST", q.path(n)+"/points/query", map[string]any{"query": v, "filter": filter, "limit": limit, "with_payload": true, "with_vector": true, "params": map[string]any{"hnsw_ef": 128, "exact": false}}, &out)
	return out.Result.Points, err
}
func (q *Qdrant) Ping(ctx context.Context) error { return q.call(ctx, "GET", "/readyz", nil, nil) }
