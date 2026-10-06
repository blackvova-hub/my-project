// Package service reads only arbitrage's namespace. Existing scanners are independent.
package service

import (
	"context"
	"encoding/json"
	"github.com/redis/go-redis/v9"
	"net/http"
	"regexp"
	"shortlong/arbitrage/internal/collector"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"shortlong/arbitrage/internal/verification"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Point struct {
	At     int64   `json:"at"`
	Spread float64 `json:"spread"`
}
type Depth struct {
	Bids []market.Level `json:"bids"`
	Asks []market.Level `json:"asks"`
}
type Snapshot struct {
	ExternalSources  []market.ExternalStatus `json:"externalSources,omitempty"`
	Total            int                     `json:"total"`
	Offset           int                     `json:"offset"`
	UniverseMode     string                  `json:"universeMode"`
	Books            map[string]Depth        `json:"books"`
	Rows             []engine.Opportunity    `json:"rows"`
	Sources          []market.Status         `json:"sources"`
	At               int64                   `json:"at"`
	Assets           []string                `json:"assets"`
	ConfiguredAssets []string                `json:"configuredAssets"`
	Transport        string                  `json:"transport"`
}
type Service struct {
	Verifier     *verification.Provider
	Alternatives func() market.AlternativeData
	Redis        *redis.Client
	Universe     []string
	mu           sync.RWMutex
	books        []market.Book
	sources      []market.Status
	since        map[string]int64
	at           int64
	transport    string
}

func New(r *redis.Client) *Service {
	assets := []string{}
	for a := range market.AssetSet(market.DefaultAssets) {
		if a != "*" {
			assets = append(assets, a)
		}
	}
	sort.Strings(assets)
	return &Service{Redis: r, Universe: assets, since: map[string]int64{}, transport: "connecting"}
}
func (s *Service) Refresh(ctx context.Context, now time.Time) error {
	pipe := s.Redis.Pipeline()
	books := map[string]*redis.MapStringStringCmd{}
	statuses := map[string]*redis.StringCmd{}
	for _, e := range market.Exchanges {
		books[e] = pipe.HGetAll(ctx, collector.Prefix+"books:"+e)
		for _, k := range []string{"spot", "perpetual"} {
			statuses[e+":"+k] = pipe.Get(ctx, collector.Prefix+"status:"+e+":"+k)
		}
	}
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		s.mu.Lock()
		s.books = nil
		s.at = now.UnixMilli()
		s.transport = "error"
		clear(s.since)
		s.mu.Unlock()
		return err
	}
	out := []market.Book{}
	sources := []market.Status{}
	for _, e := range market.Exchanges {
		decoded := map[string][]market.Book{}
		for _, raw := range books[e].Val() {
			var b market.Book
			if json.Unmarshal([]byte(raw), &b) == nil && b.Instrument.Exchange == e && b.Fresh(now, 30*time.Second) {
				decoded[b.Instrument.Market] = append(decoded[b.Instrument.Market], b)
			}
		}
		for _, k := range []string{"spot", "perpetual"} {
			st := market.Status{Exchange: e, Market: k, State: "offline", Message: "Нет свежего состояния сборщика"}
			raw, er := statuses[e+":"+k].Result()
			if er == nil {
				var v market.Status
				if json.Unmarshal([]byte(raw), &v) == nil && v.Exchange == e && v.Market == k && now.UnixMilli()-v.UpdatedAt <= 10000 && v.UpdatedAt <= now.UnixMilli()+2000 {
					st = v
				}
			}
			st.Books = 0
			if st.State == "live" {
				out = append(out, decoded[k]...)
				st.Books = len(decoded[k])
				if st.Books == 0 {
					st.State = "stale"
					st.Message = "Котировки устарели или ещё не получены"
				}
			}
			sources = append(sources, st)
		}
	}
	// Duration means continuous positive top-of-book spread, sampled once per second.
	live := map[string]int64{}
	groups := map[string][]market.Book{}
	for _, b := range out {
		if !b.Fresh(now, 10*time.Second) {
			continue
		}
		groups[b.Instrument.Identity()] = append(groups[b.Instrument.Identity()], b)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, group := range groups {
		for _, b := range group {
			for _, sell := range group {
				if b.Instrument.ID == sell.Instrument.ID || sell.Bids[0].Price <= b.Asks[0].Price {
					continue
				}
				id := engine.RouteID(b.Instrument.ID, sell.Instrument.ID)
				at := s.since[id]
				if at == 0 {
					at = now.UnixMilli()
				}
				live[id] = at
			}
		}
	}
	s.books = out
	s.sources = sources
	s.since = live
	s.at = now.UnixMilli()
	s.transport = "live"
	return nil
}
func (s *Service) Dataset(q engine.Query, now time.Time) Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d := Snapshot{Rows: []engine.Opportunity{}, Sources: append([]market.Status{}, s.sources...), At: now.UnixMilli(), Assets: []string{}, Transport: s.transport}
	d.Books = map[string]Depth{}
	d.ConfiguredAssets = append([]string{}, s.Universe...)
	d.UniverseMode = "all"
	if len(s.Universe) > 0 {
		d.UniverseMode = "selected"
	}
	if now.UnixMilli()-s.at > 3000 {
		d.Transport = "stale"
		return d
	}
	assets := map[string]bool{}
	for _, b := range s.books {
		assets[b.Instrument.Asset] = true
	}
	for a := range assets {
		d.Assets = append(d.Assets, a)
	}
	sort.Strings(d.Assets)
	limit := q.Limit
	q.Limit = 0 // Sort and count the complete universe before pagination.
	if q.Strategy == "redemption" || q.Strategy == "hedged-lp" {
		aux := market.AlternativeData{}
		if s.Alternatives != nil {
			aux = s.Alternatives()
		}
		d.ExternalSources = aux.Sources
		d.Rows = engine.ScanAlternatives(s.books, q, aux, now)
	} else {
		d.Rows = engine.Scan(s.books, q, now)
	}
	for n := range d.Rows {
		d.Rows[n].Since = s.since[d.Rows[n].ID]
	}
	if q.Sort == "duration" {
		sort.SliceStable(d.Rows, func(i, j int) bool {
			a, b := d.Rows[i].Since, d.Rows[j].Since
			if a == 0 {
				return false
			}
			if b == 0 {
				return true
			}
			if a == b {
				return d.Rows[i].ID < d.Rows[j].ID
			}
			return a < b
		})
	}
	d.Total = len(d.Rows)
	d.Offset = q.Offset
	if d.Total == 0 {
		d.Offset = 0
	} else if d.Offset >= d.Total {
		d.Offset = ((d.Total - 1) / limit) * limit
	}
	d.Rows = d.Rows[d.Offset:min(d.Offset+limit, d.Total)]
	// A book is transmitted once, even when it participates in dozens of routes.
	// All rows and depth therefore still belong to the same calculation snapshot.
	for n := range d.Rows {
		o := &d.Rows[n]
		d.Books[o.Buy.Instrument.ID] = Depth{o.Buy.Bids, o.Buy.Asks}
		d.Books[o.Sell.Instrument.ID] = Depth{o.Sell.Bids, o.Sell.Asks}
		o.Buy.Bids = nil
		o.Buy.Asks = nil
		o.Sell.Bids = nil
		o.Sell.Asks = nil
	}
	for n := range d.Sources {
		st := &d.Sources[n]
		st.Books = 0
		st.FundingBooks = 0
		for _, b := range s.books {
			if b.Instrument.Exchange == st.Exchange && b.Instrument.Market == st.Market && b.Fresh(now, time.Duration(q.MaxAgeMS)*time.Millisecond) {
				st.Books++
				if f := b.Funding; f != nil && market.Finite(f.Rate) && f.NextAt > now.UnixMilli() && f.IntervalMS >= 3600000 && f.IntervalMS <= 86400000 && f.ObservedAt <= now.UnixMilli()+2000 && now.UnixMilli()-f.ObservedAt <= 120000 {
					st.FundingBooks++
				}
			}
		}
		if st.State == "live" && st.Books == 0 {
			st.State = "stale"
		}
	}
	return d
}
func (s *Service) Sample(ctx context.Context, now time.Time) error {
	q := engine.Defaults()
	q.MinSpread = -1
	q.Limit = 300
	pipe := s.Redis.Pipeline()
	for _, kind := range []string{"spot-perpetual", "perpetual-perpetual", "spot-spot"} {
		q.Strategy = kind
		for _, o := range s.Dataset(q, now).Rows {
			p, _ := json.Marshal(Point{now.UnixMilli(), o.QuoteSpreadPct})
			key := collector.Prefix + "history:" + o.ID
			pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixMilli()), Member: string(p)})
			pipe.ZRemRangeByScore(ctx, key, "-inf", strconv.FormatInt(now.Add(-6*time.Hour).UnixMilli(), 10))
			pipe.Expire(ctx, key, 7*time.Hour)
		}
	}
	_, err := pipe.Exec(ctx)
	return err
}
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastSample time.Time
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, 2*time.Second)
		now := time.Now()
		err := s.Refresh(op, now)
		cancel()
		if err == nil && now.Sub(lastSample) >= 30*time.Second {
			op, cancel = context.WithTimeout(ctx, 3*time.Second)
			_ = s.Sample(op, now)
			cancel()
			lastSample = now
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

var routePattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /verification", s.verify)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		at := s.at
		state := s.transport
		s.mu.RUnlock()
		code := 200
		if state != "live" || time.Now().UnixMilli()-at > 3000 {
			code = 503
		}
		reply(w, code, map[string]any{"transport": state, "at": at})
	})
	mux.HandleFunc("GET /opportunities", func(w http.ResponseWriter, r *http.Request) {
		q, err := engine.Parse(r.URL.Query())
		if err != nil {
			reply(w, 400, map[string]string{"error": err.Error()})
			return
		}
		d := s.Dataset(q, time.Now())
		if q.Budget || engine.IsSpecial(q.Strategy) {
			// Request deeper books even when the threshold currently hides a
			// candidate. Otherwise a one-level feed could never pass the filter.
			demand := q
			demand.MinAvailable = 0
			demand.Limit = 6
			s.queueDepth(r.Context(), s.Dataset(demand, time.Now()).Rows)
		}
		reply(w, 200, d)
	})
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !routePattern.MatchString(id) {
			reply(w, 400, map[string]string{"error": "Некорректная связка"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		now := time.Now()
		rows, err := s.Redis.ZRangeByScore(ctx, collector.Prefix+"history:"+id, &redis.ZRangeBy{Min: strconv.FormatInt(now.Add(-6*time.Hour).UnixMilli(), 10), Max: strconv.FormatInt(now.UnixMilli(), 10), Count: 721}).Result()
		if err != nil {
			reply(w, 503, map[string]string{"error": "История временно недоступна"})
			return
		}
		points := []Point{}
		for _, raw := range rows {
			var p Point
			if json.Unmarshal([]byte(raw), &p) == nil {
				points = append(points, p)
			}
		}
		reply(w, 200, map[string]any{"points": points, "intervalMs": 30000, "retentionHours": 6, "sampled": true})
	})
	return mux
}
