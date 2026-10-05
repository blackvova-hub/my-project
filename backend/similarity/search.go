package similarity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"math"
	bt "shortlong/backtest"
	"shortlong/backtest/engine"
	"shortlong/backtest/marketdata"
	"sort"
	"strings"
	"time"
)

type Request struct {
	Market  string   `json:"market"`
	Symbol  string   `json:"symbol"`
	Window  int      `json:"windowBars"`
	End     int64    `json:"end"`
	Scope   string   `json:"scope"`
	Sector  string   `json:"sector,omitempty"`
	Symbols []string `json:"symbols,omitempty"`
	Limit   int      `json:"limit"`
}

func (r *Request) Validate(c Config, now time.Time) error {
	r.Symbol = strings.ToUpper(strings.TrimSpace(r.Symbol))
	if r.Market != "spot" && r.Market != "linear" {
		return fmt.Errorf("Выберите Spot или Futures")
	}
	if !bt.ValidSymbol(r.Symbol) {
		return fmt.Errorf("Некорректная монета")
	}
	if _, ok := c.Window(r.Window); !ok {
		return fmt.Errorf("Размер окна не поддерживается")
	}
	closed := now.Add(-10*time.Second).UnixMilli() / Step * Step
	if r.End == 0 {
		r.End = closed
	}
	if r.End <= int64(2*r.Window)*Step || r.End > closed || r.End%Step != 0 {
		return fmt.Errorf("Ожидается граница закрытых свечей UTC")
	}
	if r.Limit == 0 {
		r.Limit = 20
	}
	if r.Limit < 1 || r.Limit > 50 {
		return fmt.Errorf("Допустимо от 1 до 50 совпадений")
	}
	switch r.Scope {
	case "same_asset", "all_crypto", "alts":
		r.Sector = ""
		r.Symbols = nil
	case "sector":
		r.Sector = strings.TrimSpace(strings.ToLower(r.Sector))
		r.Symbols = nil
		if r.Sector == "" || len(r.Sector) > 64 {
			return fmt.Errorf("Выберите категорию")
		}
	case "custom":
		if len(r.Symbols) == 0 || len(r.Symbols) > 100 {
			return fmt.Errorf("Выберите до 100 монет")
		}
		for i, s := range r.Symbols {
			r.Symbols[i] = strings.ToUpper(strings.TrimSpace(s))
			if !bt.ValidSymbol(r.Symbols[i]) {
				return fmt.Errorf("Некорректная монета списка")
			}
		}
		sort.Strings(r.Symbols)
		r.Symbols = unique(r.Symbols)
		r.Sector = ""
	default:
		return fmt.Errorf("Некорректная область поиска")
	}
	return nil
}
func unique(a []string) []string {
	out := []string{}
	for _, v := range a {
		if len(out) == 0 || out[len(out)-1] != v {
			out = append(out, v)
		}
	}
	return out
}
func Allowed(assets []Asset, r Request) []string {
	custom := map[string]bool{}
	for _, s := range r.Symbols {
		custom[s] = true
	}
	out := []string{}
	for _, a := range assets {
		if a.Market != r.Market || !a.Enabled {
			continue
		}
		ok := false
		switch r.Scope {
		case "same_asset":
			ok = a.Symbol == r.Symbol
		case "all_crypto":
			ok = !a.Stable
		case "alts":
			ok = a.Alt && !a.Stable
		case "sector":
			for _, s := range a.Sectors {
				if s == r.Sector {
					ok = !a.Stable
					break
				}
			}
		case "custom":
			ok = custom[a.Symbol] && !a.Stable
		}
		if ok {
			out = append(out, a.Symbol)
		}
	}
	sort.Strings(out)
	return out
}

type Match struct {
	ID         string     `json:"id"`
	Market     string     `json:"market"`
	Symbol     string     `json:"symbol"`
	Start      int64      `json:"start"`
	End        int64      `json:"end"`
	Score      float64    `json:"score"`
	Breakdown  Breakdown  `json:"breakdown"`
	Trend      string     `json:"trend"`
	Volatility string     `json:"volatility"`
	Volume     string     `json:"volume"`
	Outcomes   []Outcome  `json:"outcomes"`
	Chart      []ChartBar `json:"chart"`
}
type Response struct {
	Query          Request `json:"query"`
	FeatureVersion string  `json:"featureVersion"`
	Candidates     int     `json:"candidates"`
	Matches        []Match `json:"matches"`
	GeneratedAt    int64   `json:"generatedAt"`
	Notice         string  `json:"notice"`
}
type Searcher struct {
	Store    Store
	Storage  *marketdata.ClickHouse
	Index    VectorIndex
	Outcomes Outcomes
	Cache    *redis.Client
	History  *marketdata.History
	slots    chan struct{}
}

func NewSearcher(s Store, ch *marketdata.ClickHouse, index VectorIndex, cache *redis.Client) *Searcher {
	return &Searcher{Store: s, Storage: ch, Index: index, Outcomes: Outcomes{ch}, Cache: cache, slots: make(chan struct{}, 4)}
}
func Deduplicate(matches []Match, r Request) []Match {
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Score == matches[j].Score {
			return matches[i].ID < matches[j].ID
		}
		return matches[i].Score > matches[j].Score
	})
	out := []Match{}
	counts := map[string]int{}
	events := map[int64]int{}
	duration := int64(r.Window) * Step
	for _, m := range matches {
		skip := false
		for _, kept := range out {
			if kept.Symbol == m.Symbol && kept.Market == m.Market && m.Start < kept.End && kept.Start < m.End {
				skip = true
				break
			}
		}
		bucket := m.End / duration
		if r.Scope != "same_asset" && (counts[m.Symbol] >= 3 || events[bucket] >= 3) {
			skip = true
		}
		if skip {
			continue
		}
		out = append(out, m)
		counts[m.Symbol]++
		events[bucket]++
		if len(out) == r.Limit {
			break
		}
	}
	return out
}
func (s *Searcher) compute(ctx context.Context, r Request, allowed []string) (Response, error) {
	out := Response{Query: r, FeatureVersion: s.Store.Config.Version, Matches: []Match{}, GeneratedAt: time.Now().UnixMilli(), Notice: "Сходство не является вероятностью прогноза. История подготовлена по запросу. Прочерк в исходах означает, что горизонт ещё не завершён или свечи неполны."}
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return out, ctx.Err()
	}
	from := r.End - int64(2*r.Window)*Step
	c, e := s.Storage.Read(ctx, r.Market, r.Symbol, "5m", from, r.End)
	// The bulk archive refreshes twice daily. Only an explicit search fills the
	// selected asset's missing query candles, under the shared history lock/limiter.
	if e == nil && s.History != nil && engine.ValidateCandles(c, from, r.End, Step) != nil {
		c, e = s.History.Load(ctx, r.Market, r.Symbol, "5m", from, r.End, nil)
	}
	if e != nil {
		return out, e
	}
	signature, e := Features(c, r.Window)
	if e != nil {
		return out, fmt.Errorf("Для выбранного окна и предыдущего режима ещё нет непрерывной истории 5m")
	}
	points, e := s.Index.Search(ctx, r.Window, signature.Vector(s.Store.Config.Weights), r.Market, allowed, r.End-int64(r.Window)*Step, s.Store.Config.Candidates)
	if e != nil {
		return out, e
	}
	out.Candidates = len(points)
	candidates := []Match{}
	for _, p := range points {
		other, e := FromVector(p.Vector, s.Store.Config.Weights)
		if e != nil {
			return out, e
		}
		score, breakdown := Compare(signature, other, s.Store.Config.Weights)
		if math.IsNaN(score) {
			continue
		}
		candidates = append(candidates, Match{ID: p.ID, Market: p.Payload.Market, Symbol: p.Payload.Symbol, Start: p.Payload.End - int64(r.Window)*Step, End: p.Payload.End, Score: score, Breakdown: breakdown, Trend: p.Payload.Trend, Volatility: p.Payload.Volatility, Volume: p.Payload.Volume, Outcomes: []Outcome{}})
	}
	out.Matches = Deduplicate(candidates, r)
	// Outcomes are read only AFTER ranking/deduplication, and never after query as-of.
	for i, m := range out.Matches {
		chartEnd := min(r.End, m.End+int64(min(r.Window, 288))*Step)
		futureEnd := min(r.End, m.End+int64(Horizons[len(Horizons)-1])*Step)
		bars, err := s.Storage.Read(ctx, m.Market, m.Symbol, "5m", min(m.Start, m.End-Step), futureEnd)
		if err != nil {
			return out, err
		}
		future := []bt.Candle{}
		chart := []bt.Candle{}
		for _, bar := range bars {
			if bar.Time >= m.End-Step {
				future = append(future, bar)
			}
			if bar.Time >= m.Start && bar.Time < chartEnd {
				chart = append(chart, bar)
			}
		}
		for _, h := range Horizons {
			needed := h + 1
			if m.End+int64(h)*Step <= r.End && len(future) >= needed {
				if o, err := Future(future[:needed], m.End, h); err == nil {
					out.Matches[i].Outcomes = append(out.Matches[i].Outcomes, o)
				}
			}
		}
		out.Matches[i].Chart = ChartBars(chart, max(1, (r.Window+47)/48), m.End)
	}
	return out, nil
}

var unlock = redis.NewScript(`if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) end return 0`)
var publish = redis.NewScript(`if redis.call('get',KEYS[1])==ARGV[1] then redis.call('set',KEYS[2],ARGV[2],'EX',60); redis.call('del',KEYS[1]); return 1 end return 0`)

func (s *Searcher) Search(ctx context.Context, r Request) ([]byte, error) {
	assets, e := s.Store.Assets(ctx)
	if e != nil {
		return nil, e
	}
	allowed := Allowed(assets, r)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("В выбранной области пока нет доступных монет; проверьте метаданные категории")
	}
	raw, _ := json.Marshal(struct {
		Request Request
		Allowed []string
		Version string
	}{r, allowed, s.Store.Config.Version})
	hash := sha256.Sum256(raw)
	key := "similarity:search:" + hex.EncodeToString(hash[:])
	lock := key + ":lock"
	nonce := make([]byte, 16)
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	token := hex.EncodeToString(nonce)
	for {
		cached, e := s.Cache.Get(ctx, key).Bytes()
		if e == nil {
			return cached, nil
		}
		if e != redis.Nil {
			return nil, e
		}
		ok, e := s.Cache.SetNX(ctx, lock, token, 75*time.Second).Result()
		if e != nil {
			return nil, e
		}
		if ok {
			defer func() {
				release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = unlock.Run(release, s.Cache, []string{lock}, token).Err()
			}()
			cached, e = s.Cache.Get(ctx, key).Bytes()
			if e == nil {
				return cached, nil
			}
			if e != redis.Nil {
				return nil, e
			}
			work, cancel := context.WithTimeout(ctx, 60*time.Second)
			result, e := s.compute(work, r, allowed)
			cancel()
			if e != nil {
				return nil, e
			}
			raw, e = json.Marshal(result)
			if e != nil {
				return nil, e
			}
			n, e := publish.Run(ctx, s.Cache, []string{lock, key}, token, string(raw)).Int()
			if e != nil {
				return nil, e
			}
			if n != 1 {
				return nil, fmt.Errorf("Истёк срок общего запроса, повторите поиск")
			}
			return raw, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
