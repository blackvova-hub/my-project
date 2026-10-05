package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const liquidationCatalogRefreshInterval = 30 * time.Minute

type liquidationInstrument struct {
	CanonicalSymbol string
	NativeSymbol    string
	ContractValue   float64
}

type liquidationCatalog struct {
	mu       sync.RWMutex
	byNative map[string]liquidationInstrument
	symbols  []string
}

func newLiquidationCatalog(instruments []liquidationInstrument) *liquidationCatalog {
	catalog := &liquidationCatalog{}
	catalog.replace(instruments)
	return catalog
}

func (c *liquidationCatalog) replace(instruments []liquidationInstrument) {
	byNative := make(map[string]liquidationInstrument, len(instruments))
	symbolSet := make(map[string]struct{}, len(instruments))
	for _, instrument := range instruments {
		instrument.CanonicalSymbol = normalizeSymbol(instrument.CanonicalSymbol)
		instrument.NativeSymbol = strings.ToUpper(strings.TrimSpace(instrument.NativeSymbol))
		if instrument.CanonicalSymbol == "" || instrument.NativeSymbol == "" {
			continue
		}
		byNative[instrument.NativeSymbol] = instrument
		symbolSet[instrument.CanonicalSymbol] = struct{}{}
	}
	symbols := make([]string, 0, len(symbolSet))
	for symbol := range symbolSet {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	c.mu.Lock()
	c.byNative = byNative
	c.symbols = symbols
	c.mu.Unlock()
}

func (c *liquidationCatalog) lookup(nativeSymbol string) (liquidationInstrument, bool) {
	c.mu.RLock()
	instrument, ok := c.byNative[strings.ToUpper(strings.TrimSpace(nativeSymbol))]
	c.mu.RUnlock()
	return instrument, ok
}

func (c *liquidationCatalog) snapshotSymbols() []string {
	c.mu.RLock()
	symbols := append([]string(nil), c.symbols...)
	c.mu.RUnlock()
	return symbols
}

type liquidationFeed interface {
	Exchange() Exchange
	Source() string
	LoadInstruments(context.Context) ([]liquidationInstrument, error)
	Consume(context.Context, *liquidationCatalog, *LiquidationStore) error
}

type baseLiquidationFeed struct {
	cfg    Config
	client *http.Client
}

func newBaseLiquidationFeed(cfg Config) baseLiquidationFeed {
	return baseLiquidationFeed{cfg: cfg, client: &http.Client{Timeout: cfg.HTTPTimeout}}
}

func isLiquidationOnlyExchange(exchange Exchange) bool {
	switch exchange {
	case ExchangeOKX, ExchangeBitget, ExchangeGateIO:
		return true
	default:
		return false
	}
}

func newLiquidationFeed(cfg Config) (liquidationFeed, error) {
	base := newBaseLiquidationFeed(cfg)
	switch normalizeExchange(cfg.Exchange) {
	case ExchangeOKX:
		return &okxLiquidationFeed{baseLiquidationFeed: base}, nil
	case ExchangeBitget:
		return &bitgetLiquidationFeed{baseLiquidationFeed: base}, nil
	case ExchangeGateIO:
		return &gateLiquidationFeed{baseLiquidationFeed: base}, nil
	default:
		return nil, fmt.Errorf("exchange %q is not liquidation-only", cfg.Exchange)
	}
}

func (b *baseLiquidationFeed) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := strings.TrimRight(b.cfg.BaseURL, "/") + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "shortlong-liquidations/1.0")
	resp, err := b.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GET %s: status %d", path, resp.StatusCode)
	}
	return decodeBoundedJSON(resp.Body, out)
}

func (b *baseLiquidationFeed) dial(ctx context.Context, header http.Header) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		Proxy:            http.ProxyFromEnvironment,
		TLSClientConfig:  &tls.Config{MinVersion: tls.VersionTLS12},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.DialContext(ctx, b.cfg.WSURL, header)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(8 << 20)
	return conn, nil
}

func runLiquidationOnlyScanner(ctx context.Context, cfg Config, publisher Publisher) error {
	feed, err := newLiquidationFeed(cfg)
	if err != nil {
		return err
	}
	instruments, err := loadLiquidationInstrumentsWithRetry(ctx, feed, cfg.SymbolsLoadRetries)
	if err != nil {
		return err
	}
	catalog := newLiquidationCatalog(instruments)
	if len(catalog.snapshotSymbols()) == 0 {
		return errors.New("liquidation instrument catalog is empty")
	}
	store := NewLiquidationStore()
	log.Printf("liquidation-only catalog exchange=%s symbols=%d", feed.Exchange(), len(catalog.snapshotSymbols()))

	go refreshLiquidationCatalog(ctx, feed, catalog)
	go runLiquidationFeed(ctx, feed, catalog, store)

	settleDelay := time.Duration(cfg.CandleSettleDelaySeconds) * time.Second
	nextRun := time.Now().UTC().Truncate(time.Minute).Add(time.Minute).Add(settleDelay)
	for waitUntil(ctx, nextRun) {
		targetMinute := nextRun.Truncate(time.Minute).Add(-time.Minute).UnixMilli()
		if err := publishLiquidationMinute(ctx, publisher, feed, catalog, store, targetMinute); err != nil {
			return err
		}
		store.Cleanup(minuteKey(targetMinute) - 120)
		nextRun = nextRun.Add(time.Minute)
		now := time.Now().UTC()
		if !nextRun.After(now) {
			skipped := int(now.Sub(nextRun)/time.Minute) + 1
			log.Printf("liquidation-only scanner fell behind exchange=%s skipped=%d", feed.Exchange(), skipped)
			nextRun = nextRun.Add(time.Duration(skipped) * time.Minute)
		}
	}
	return ctx.Err()
}

func loadLiquidationInstrumentsWithRetry(ctx context.Context, feed liquidationFeed, attempts int) ([]liquidationInstrument, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		instruments, err := feed.LoadInstruments(ctx)
		if err == nil && len(instruments) > 0 {
			return instruments, nil
		}
		if err == nil {
			err = errors.New("empty instrument response")
		}
		lastErr = err
		if attempt < attempts {
			timer := time.NewTimer(time.Duration(attempt*2) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, fmt.Errorf("load %s liquidation instruments: %w", feed.Exchange(), lastErr)
}

func refreshLiquidationCatalog(ctx context.Context, feed liquidationFeed, catalog *liquidationCatalog) {
	ticker := time.NewTicker(liquidationCatalogRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			instruments, err := feed.LoadInstruments(refreshCtx)
			cancel()
			if err != nil || len(instruments) == 0 {
				log.Printf("liquidation catalog refresh exchange=%s error=%v", feed.Exchange(), err)
				continue
			}
			catalog.replace(instruments)
			log.Printf("liquidation catalog refreshed exchange=%s symbols=%d", feed.Exchange(), len(catalog.snapshotSymbols()))
		}
	}
}

func runLiquidationFeed(ctx context.Context, feed liquidationFeed, catalog *liquidationCatalog, store *LiquidationStore) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := feed.Consume(ctx, catalog, store)
		store.SetConnected(false)
		if ctx.Err() != nil {
			return
		}
		log.Printf("liquidation websocket exchange=%s error=%v", feed.Exchange(), err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func publishLiquidationMinute(ctx context.Context, publisher Publisher, feed liquidationFeed, catalog *liquidationCatalog, store *LiquidationStore, minuteStartMs int64) error {
	covered := store.CoversMinute(minuteStartMs)
	symbols := catalog.snapshotSymbols()
	compactRows := make([]compactLiquidationRow, 0)
	for _, symbol := range symbols {
		instrument := NewInstrumentKey(feed.Exchange(), MarketTypePerpetual, symbol)
		bucket := store.Get(instrument, minuteStartMs)
		if bucket.Total != 0 || bucket.Long != 0 || bucket.Short != 0 || bucket.Count != 0 || bucket.Largest != 0 {
			compactRows = append(compactRows, compactLiquidationRow{Symbol: symbol, Values: [5]float64{round6(bucket.Total), round6(bucket.Long), round6(bucket.Short), round6(bucket.Count), round6(bucket.Largest)}})
		}
	}
	if publisher != nil {
		batch, err := buildCompactLiquidationBatch(compactRows, symbols, minuteStartMs, covered, feed.Source(), string(feed.Exchange()), string(MarketTypePerpetual))
		if err != nil {
			log.Printf("compact liquidation build failed exchange=%s minute=%d err=%v", feed.Exchange(), minuteStartMs, err)
			return err
		} else if raw, marshalErr := json.Marshal(batch); marshalErr != nil {
			log.Printf("compact liquidation encode failed exchange=%s minute=%d err=%v", feed.Exchange(), minuteStartMs, marshalErr)
			return marshalErr
		} else if publishErr := publisher.Publish(ctx, raw); publishErr != nil {
			log.Printf("compact liquidation publish failed exchange=%s minute=%d err=%v", feed.Exchange(), minuteStartMs, publishErr)
			return publishErr
		} else {
			log.Printf("compact liquidation published exchange=%s minute=%d rows=%d catalog=%d coverage=%v bytes=%d", feed.Exchange(), minuteStartMs, len(batch.LiquidationRows), len(batch.Catalog), batch.Coverage, len(raw))
		}
	}
	log.Printf("liquidation minute published exchange=%s minute=%s symbols=%d covered=%v", feed.Exchange(), time.UnixMilli(minuteStartMs).UTC().Format(time.RFC3339), len(symbols), covered)
	return nil
}

func websocketReadLoopContext(ctx context.Context, conn *websocket.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() { close(done) }
}

func startTextHeartbeat(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, interval time.Duration, payload func() any) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			writeMu.Lock()
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			value := payload()
			var writeErr error
			if text, ok := value.(string); ok {
				writeErr = conn.WriteMessage(websocket.TextMessage, []byte(text))
			} else {
				writeErr = conn.WriteJSON(value)
			}
			writeMu.Unlock()
			if writeErr != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func refreshWebsocketReadDeadline(conn *websocket.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
}

type okxLiquidationFeed struct{ baseLiquidationFeed }

func (f *okxLiquidationFeed) Exchange() Exchange { return ExchangeOKX }
func (f *okxLiquidationFeed) Source() string     { return "okx:websocket_snapshot" }

func (f *okxLiquidationFeed) LoadInstruments(ctx context.Context) ([]liquidationInstrument, error) {
	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			InstType  string `json:"instType"`
			InstID    string `json:"instId"`
			BaseCcy   string `json:"baseCcy"`
			QuoteCcy  string `json:"quoteCcy"`
			SettleCcy string `json:"settleCcy"`
			CtType    string `json:"ctType"`
			CtVal     string `json:"ctVal"`
			CtValCcy  string `json:"ctValCcy"`
			State     string `json:"state"`
		} `json:"data"`
	}
	if err := f.getJSON(ctx, "/api/v5/public/instruments", url.Values{"instType": {"SWAP"}}, &response); err != nil {
		return nil, err
	}
	if response.Code != "0" {
		return nil, fmt.Errorf("okx instruments code=%s msg=%s", response.Code, response.Msg)
	}
	if len(response.Data) > maxScannerCatalogSymbols {
		return nil, errors.New("okx instrument catalog exceeds safe symbol limit")
	}
	out := make([]liquidationInstrument, 0, len(response.Data))
	for _, item := range response.Data {
		contractValue, err := parseFiniteFloat(item.CtVal)
		if err != nil || contractValue <= 0 || !strings.EqualFold(item.InstType, "SWAP") || !strings.EqualFold(item.SettleCcy, "USDT") || !strings.EqualFold(item.State, "live") || (item.CtType != "" && !strings.EqualFold(item.CtType, "linear")) {
			continue
		}
		baseCurrency := item.BaseCcy
		quoteCurrency := item.QuoteCcy
		parts := strings.Split(strings.ToUpper(item.InstID), "-")
		if len(parts) >= 3 {
			if baseCurrency == "" {
				baseCurrency = parts[0]
			}
			if quoteCurrency == "" {
				quoteCurrency = parts[1]
			}
		}
		if baseCurrency == "" || quoteCurrency == "" || !strings.EqualFold(item.CtValCcy, baseCurrency) {
			continue
		}
		canonical := normalizeSymbol(baseCurrency + quoteCurrency)
		out = append(out, liquidationInstrument{CanonicalSymbol: canonical, NativeSymbol: item.InstID, ContractValue: contractValue})
	}
	return out, nil
}

func (f *okxLiquidationFeed) Consume(ctx context.Context, catalog *liquidationCatalog, store *LiquidationStore) error {
	conn, err := f.dial(ctx, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := websocketReadLoopContext(ctx, conn)
	defer stopClose()
	var writeMu sync.Mutex
	if err := conn.WriteJSON(map[string]any{"id": "liquidations", "op": "subscribe", "args": []map[string]string{{"channel": "liquidation-orders", "instType": "SWAP"}}}); err != nil {
		return err
	}
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go startTextHeartbeat(connCtx, conn, &writeMu, 20*time.Second, func() any { return "ping" })
	refreshWebsocketReadDeadline(conn)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		refreshWebsocketReadDeadline(conn)
		if string(raw) == "pong" {
			continue
		}
		var message struct {
			Event string `json:"event"`
			Code  string `json:"code"`
			Msg   string `json:"msg"`
			Arg   struct {
				Channel string `json:"channel"`
			} `json:"arg"`
			Data []struct {
				InstID  string `json:"instId"`
				Details []struct {
					Side    string `json:"side"`
					PosSide string `json:"posSide"`
					Price   string `json:"bkPx"`
					Size    string `json:"sz"`
					TS      string `json:"ts"`
				} `json:"details"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.Event == "error" || (message.Event != "" && message.Code != "" && message.Code != "0") {
			return fmt.Errorf("okx subscription code=%s msg=%s", message.Code, message.Msg)
		}
		if message.Event == "subscribe" && message.Arg.Channel == "liquidation-orders" {
			store.SetConnected(true)
			continue
		}
		for _, item := range message.Data {
			instrument, ok := catalog.lookup(item.InstID)
			if !ok {
				continue
			}
			for _, detail := range item.Details {
				price, priceErr := parseFiniteFloat(detail.Price)
				size, sizeErr := parseFiniteFloat(detail.Size)
				ts, tsErr := strconv.ParseInt(detail.TS, 10, 64)
				side, sideOK := okxCanonicalLiquidationSide(detail.PosSide, detail.Side)
				usd := price * size * instrument.ContractValue
				if priceErr != nil || sizeErr != nil || tsErr != nil || !sideOK || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
					continue
				}
				store.Add(NewInstrumentKey(ExchangeOKX, MarketTypePerpetual, instrument.CanonicalSymbol), ts, usd, side)
			}
		}
	}
}

func okxCanonicalLiquidationSide(positionSide, orderSide string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(positionSide)) {
	case "long":
		return "Buy", true
	case "short":
		return "Sell", true
	}
	switch strings.ToLower(strings.TrimSpace(orderSide)) {
	case "sell":
		return "Buy", true
	case "buy":
		return "Sell", true
	default:
		return "", false
	}
}

type bitgetLiquidationFeed struct{ baseLiquidationFeed }

func (f *bitgetLiquidationFeed) Exchange() Exchange { return ExchangeBitget }
func (f *bitgetLiquidationFeed) Source() string     { return "bitget:websocket_1s_snapshot" }

func (f *bitgetLiquidationFeed) LoadInstruments(ctx context.Context) ([]liquidationInstrument, error) {
	var response struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data []struct {
			Symbol    string `json:"symbol"`
			BaseCoin  string `json:"baseCoin"`
			QuoteCoin string `json:"quoteCoin"`
			Type      string `json:"type"`
			Status    string `json:"status"`
		} `json:"data"`
	}
	if err := f.getJSON(ctx, "/api/v3/market/instruments", url.Values{"category": {"USDT-FUTURES"}}, &response); err != nil {
		return nil, err
	}
	if response.Code != "00000" {
		return nil, fmt.Errorf("bitget instruments code=%s msg=%s", response.Code, response.Msg)
	}
	if len(response.Data) > maxScannerCatalogSymbols {
		return nil, errors.New("bitget instrument catalog exceeds safe symbol limit")
	}
	out := make([]liquidationInstrument, 0, len(response.Data))
	for _, item := range response.Data {
		if !strings.EqualFold(item.Status, "online") || !strings.EqualFold(item.Type, "perpetual") || !strings.EqualFold(item.QuoteCoin, "USDT") {
			continue
		}
		out = append(out, liquidationInstrument{CanonicalSymbol: normalizeSymbol(item.BaseCoin + item.QuoteCoin), NativeSymbol: item.Symbol, ContractValue: 1})
	}
	return out, nil
}

func (f *bitgetLiquidationFeed) Consume(ctx context.Context, catalog *liquidationCatalog, store *LiquidationStore) error {
	conn, err := f.dial(ctx, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := websocketReadLoopContext(ctx, conn)
	defer stopClose()
	var writeMu sync.Mutex
	if err := conn.WriteJSON(map[string]any{"op": "subscribe", "args": []map[string]string{{"instType": "usdt-futures", "topic": "liquidation"}}}); err != nil {
		return err
	}
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go startTextHeartbeat(connCtx, conn, &writeMu, 20*time.Second, func() any { return "ping" })
	refreshWebsocketReadDeadline(conn)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		refreshWebsocketReadDeadline(conn)
		if string(raw) == "pong" {
			continue
		}
		var message struct {
			Event string `json:"event"`
			Code  string `json:"code"`
			Msg   string `json:"msg"`
			Arg   struct {
				Topic string `json:"topic"`
			} `json:"arg"`
			Data []struct {
				Symbol string `json:"symbol"`
				Side   string `json:"side"`
				Amount string `json:"amount"`
				TS     string `json:"ts"`
			} `json:"data"`
		}
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.Code != "" && message.Code != "00000" {
			return fmt.Errorf("bitget subscription code=%s msg=%s", message.Code, message.Msg)
		}
		if message.Event == "subscribe" && message.Arg.Topic == "liquidation" {
			store.SetConnected(true)
			continue
		}
		for _, item := range message.Data {
			instrument, ok := catalog.lookup(item.Symbol)
			if !ok {
				continue
			}
			usd, amountErr := parseFiniteFloat(item.Amount)
			ts, tsErr := strconv.ParseInt(item.TS, 10, 64)
			side, sideOK := bitgetCanonicalLiquidationSide(item.Side)
			if amountErr != nil || tsErr != nil || !sideOK || usd <= 0 {
				continue
			}
			store.Add(NewInstrumentKey(ExchangeBitget, MarketTypePerpetual, instrument.CanonicalSymbol), ts, usd, side)
		}
	}
}

func bitgetCanonicalLiquidationSide(positionSide string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(positionSide)) {
	case "buy":
		return "Buy", true
	case "sell":
		return "Sell", true
	default:
		return "", false
	}
}

type gateLiquidationFeed struct{ baseLiquidationFeed }

func (f *gateLiquidationFeed) Exchange() Exchange { return ExchangeGateIO }
func (f *gateLiquidationFeed) Source() string     { return "gateio:websocket_1s_snapshot" }

func (f *gateLiquidationFeed) LoadInstruments(ctx context.Context) ([]liquidationInstrument, error) {
	var response []struct {
		Name             string `json:"name"`
		Type             string `json:"type"`
		QuantoMultiplier string `json:"quanto_multiplier"`
		Status           string `json:"status"`
	}
	if err := f.getJSON(ctx, "/api/v4/futures/usdt/contracts", nil, &response); err != nil {
		return nil, err
	}
	if len(response) > maxScannerCatalogSymbols {
		return nil, errors.New("gateio instrument catalog exceeds safe symbol limit")
	}
	out := make([]liquidationInstrument, 0, len(response))
	for _, item := range response {
		multiplier, err := parseFiniteFloat(item.QuantoMultiplier)
		if err != nil || multiplier <= 0 || !strings.EqualFold(item.Type, "direct") || !strings.EqualFold(item.Status, "trading") {
			continue
		}
		out = append(out, liquidationInstrument{CanonicalSymbol: normalizeSymbol(item.Name), NativeSymbol: item.Name, ContractValue: multiplier})
	}
	return out, nil
}

func (f *gateLiquidationFeed) Consume(ctx context.Context, catalog *liquidationCatalog, store *LiquidationStore) error {
	header := http.Header{"X-Gate-Size-Decimal": []string{"1"}}
	conn, err := f.dial(ctx, header)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := websocketReadLoopContext(ctx, conn)
	defer stopClose()
	var writeMu sync.Mutex
	if err := conn.WriteJSON(map[string]any{"time": time.Now().Unix(), "channel": "futures.public_liquidates", "event": "subscribe", "payload": []string{"!all"}}); err != nil {
		return err
	}
	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go startTextHeartbeat(connCtx, conn, &writeMu, 20*time.Second, func() any {
		return map[string]any{"time": time.Now().Unix(), "channel": "futures.ping"}
	})
	refreshWebsocketReadDeadline(conn)
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		refreshWebsocketReadDeadline(conn)
		var message struct {
			TimeMS  int64  `json:"time_ms"`
			Channel string `json:"channel"`
			Event   string `json:"event"`
			Error   *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(raw, &message) != nil {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("gateio subscription code=%d msg=%s", message.Error.Code, message.Error.Message)
		}
		if message.Channel != "futures.public_liquidates" {
			continue
		}
		if message.Event == "subscribe" {
			store.SetConnected(true)
			continue
		}
		if message.Event != "update" {
			continue
		}
		var items []struct {
			Price    string `json:"price"`
			Size     string `json:"size"`
			Time     int64  `json:"time"`
			TimeMS   int64  `json:"time_ms"`
			Contract string `json:"contract"`
		}
		if json.Unmarshal(message.Result, &items) != nil {
			continue
		}
		for _, item := range items {
			instrument, ok := catalog.lookup(item.Contract)
			if !ok {
				continue
			}
			price, priceErr := parseFiniteFloat(item.Price)
			size, sizeErr := parseFiniteFloat(item.Size)
			side, sideOK := gateCanonicalLiquidationSide(size)
			ts := item.TimeMS
			if ts <= 0 {
				ts = item.Time * 1000
			}
			if ts <= 0 {
				ts = message.TimeMS
			}
			usd := math.Abs(size) * instrument.ContractValue * price
			if priceErr != nil || sizeErr != nil || !sideOK || ts <= 0 || usd <= 0 || math.IsNaN(usd) || math.IsInf(usd, 0) {
				continue
			}
			store.Add(NewInstrumentKey(ExchangeGateIO, MarketTypePerpetual, instrument.CanonicalSymbol), ts, usd, side)
		}
	}
}

func gateCanonicalLiquidationSide(signedContracts float64) (string, bool) {
	if math.IsNaN(signedContracts) || math.IsInf(signedContracts, 0) || signedContracts == 0 {
		return "", false
	}
	if signedContracts < 0 {
		return "Buy", true
	}
	return "Sell", true
}
