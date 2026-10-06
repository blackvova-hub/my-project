package service

import (
	"context"
	"net/http"
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"shortlong/arbitrage/internal/verification"
	"sync"
	"time"
)

func (s *Service) verificationRow(q engine.Query, id string) (engine.Opportunity, market.Book, market.Book, bool) {
	// A verification cannot bypass universe, route selection, or source freshness.
	q.Routes = []string{id}
	q.MinAvailable = 0
	q.Offset = 0
	q.Amount = 100
	q.Limit = 1
	q.Budget = true
	q.Simple = true
	d := s.Dataset(q, time.Now())
	if len(d.Rows) != 1 {
		return engine.Opportunity{}, market.Book{}, market.Book{}, false
	}
	o := d.Rows[0]
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, sell := market.Book{}, market.Book{}
	for _, book := range s.books {
		if book.Instrument.ID == o.Buy.Instrument.ID {
			b = book
		}
		if book.Instrument.ID == o.Sell.Instrument.ID {
			sell = book
		}
	}
	// Synthetic issuer/pool legs stay explicitly invalid for the two-book check.
	if b.Instrument.ID == "" {
		b.Instrument = o.Buy.Instrument
	}
	if sell.Instrument.ID == "" {
		sell.Instrument = o.Sell.Instrument
	}
	return o, b, sell, true
}
func (s *Service) verify(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	q, err := engine.Parse(r.URL.Query())
	if err != nil || !routePattern.MatchString(id) {
		reply(w, 400, map[string]string{"error": "Некорректная связка или сумма проверки"})
		return
	}
	o, b, sell, ok := s.verificationRow(q, id)
	if !ok {
		reply(w, 400, map[string]string{"error": "Связка больше не входит в свежие результаты"})
		return
	}
	s.queueDepth(r.Context(), []engine.Opportunity{o})
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	be, se := market.VenueEvidence{}, market.VenueEvidence{}
	if s.Verifier != nil {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); be = s.Verifier.Evidence(ctx, b.Instrument) }()
		go func() { defer wg.Done(); se = s.Verifier.Evidence(ctx, sell.Instrument) }()
		wg.Wait()
	}
	// Read books again after metadata requests: slow HTTP never freshens prices.
	_, b, sell, ok = s.verificationRow(q, id)
	if !ok {
		reply(w, 400, map[string]string{"error": "Связка устарела во время проверки"})
		return
	}
	reply(w, 200, verification.Assess(o, b, sell, be, se, q.Amount, time.Now()))
}
