package exchanges

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"net/http"
	"net/url"
	"shortlong/arbitrage/internal/market"
	"strings"
	"sync"
	"time"
)

// Snapshot channels deliberately avoid unreliable reconstruction of partial deltas.
// Every supported depth message replaces the entire advertised top-five book.
func DecodeBook(exchange, kind string, payload []byte, instruments map[string]market.Instrument, received time.Time) (market.Book, bool, error) {
	var root Record
	d := json.NewDecoder(strings.NewReader(string(payload)))
	d.UseNumber()
	if err := d.Decode(&root); err != nil {
		return market.Book{}, false, err
	}
	if str(root, "event") == "error" || root["error"] != nil {
		return market.Book{}, false, errors.New("exchange subscription rejected")
	}
	if root["success"] == false || (exchange == "binance" && root["code"] != nil && str(root, "code") != "0") {
		return market.Book{}, false, errors.New("exchange subscription rejected")
	}
	var native string
	var bids, asks any
	var ts, seq int64
	switch exchange {
	case "binance":
		x := root
		if v := nested(root, "data"); len(v) > 0 {
			x = v
		}
		native = str(x, "s")
		if native == "" {
			native = strings.ToUpper(strings.Split(str(root, "stream"), "@")[0])
		}
		bids = x["bids"]
		asks = x["asks"]
		if bids == nil {
			bids = x["b"]
			asks = x["a"]
		}
		ts = int64(number(x, "E"))
		seq = int64(number(x, "u"))
		if seq == 0 {
			seq = int64(number(x, "lastUpdateId"))
		}
	case "bybit":
		if !strings.HasPrefix(str(root, "topic"), "orderbook.") {
			return market.Book{}, false, nil
		}
		if str(root, "type") != "snapshot" {
			return market.Book{}, false, errors.New("unexpected delta on snapshot channel")
		}
		x := nested(root, "data")
		native = str(x, "s")
		bids = x["b"]
		asks = x["a"]
		ts = int64(number(root, "cts"))
		if ts == 0 {
			ts = int64(number(root, "ts"))
		}
		seq = int64(number(x, "u"))
	case "okx", "bitget":
		arg := nested(root, "arg")
		native = str(arg, "instId")
		if native == "" {
			native = str(arg, "symbol")
		}
		rows := arr(root["data"])
		if len(rows) == 0 {
			return market.Book{}, false, nil
		}
		if action := str(root, "action"); action != "" && action != "snapshot" {
			return market.Book{}, false, errors.New("unexpected delta on snapshot channel")
		}
		x := rec(rows[0])
		bids = x["bids"]
		asks = x["asks"]
		ts = int64(number(x, "ts"))
		seq = int64(number(x, "seqId"))
		if seq == 0 {
			seq = int64(number(x, "seq"))
		}
	case "gateio":
		if root["error"] != nil {
			return market.Book{}, false, errors.New("gate subscription rejected")
		}
		wantEvent := "update"
		if kind == "perpetual" {
			wantEvent = "all"
		}
		if str(root, "event") != wantEvent {
			return market.Book{}, false, nil
		}
		x := nested(root, "result")
		native = str(x, "s")
		if native == "" {
			native = str(x, "contract")
		}
		bids = x["bids"]
		asks = x["asks"]
		ts = int64(number(x, "t"))
		if ts == 0 {
			ts = int64(number(root, "time_ms"))
		}
		seq = int64(number(x, "id"))
	case "kucoin":
		if str(root, "type") == "error" {
			return market.Book{}, false, errors.New("kucoin subscription rejected")
		}
		if str(root, "type") != "message" {
			return market.Book{}, false, nil
		}
		parts := strings.SplitN(str(root, "topic"), ":", 2)
		if len(parts) != 2 {
			return market.Book{}, false, nil
		}
		native = parts[1]
		x := nested(root, "data")
		bids = x["bids"]
		asks = x["asks"]
		ts = int64(number(x, "timestamp"))
		seq = int64(number(x, "sequence"))
	default:
		return market.Book{}, false, errors.New("unsupported exchange")
	}
	i, ok := instruments[native]
	if !ok || bids == nil || asks == nil {
		return market.Book{}, false, nil
	}
	levels := func(raw any) []market.Level {
		out := make([]market.Level, 0, 5)
		for _, v := range arr(raw) {
			price, size := 0.0, 0.0
			if pair := arr(v); len(pair) >= 2 {
				price = num(pair[0])
				size = num(pair[1])
			} else {
				x := rec(v)
				price = number(x, "p")
				size = number(x, "s")
			}
			if price <= 0 || size <= 0 {
				continue
			}
			out = append(out, market.Level{Price: price / i.PriceScale, Quantity: size * i.QuantityScale})
		}
		return out
	}
	b := market.Book{Instrument: i, Bids: levels(bids), Asks: levels(asks), SourceAt: ts, ReceivedAt: received.UnixMilli(), Sequence: seq, Valid: true}
	market.SortLevels(b.Bids, true)
	market.SortLevels(b.Asks, false)
	if err := b.Validate(); err != nil {
		return market.Book{}, false, err
	}
	return b, true, nil
}
func (c *Client) websocketURL(ctx context.Context, kind string) (string, time.Duration, error) {
	var endpoint string
	switch c.Exchange {
	case "binance":
		endpoint = "wss://stream.binance.com:443/stream"
		if kind == "perpetual" {
			endpoint = "wss://fstream.binance.com/public/stream"
		}
	case "bybit":
		endpoint = "wss://stream.bybit.com/v5/public/spot"
		if kind == "perpetual" {
			endpoint = "wss://stream.bybit.com/v5/public/linear"
		}
	case "okx":
		endpoint = "wss://ws.okx.com:8443/ws/v5/public"
	case "bitget":
		endpoint = "wss://ws.bitget.com/v2/ws/public"
	case "gateio":
		endpoint = "wss://api.gateio.ws/ws/v4/"
		if kind == "perpetual" {
			endpoint = "wss://fx-ws.gateio.ws/v4/ws/usdt"
		}
	case "kucoin":
		base := c.BaseSpot
		if kind == "perpetual" {
			base = c.BasePerpetual
		}
		v, err := c.request(ctx, "POST", base, "/api/v1/bullet-public")
		if err != nil {
			return "", 0, err
		}
		data := nested(rec(v), "data")
		servers := arr(data["instanceServers"])
		if len(servers) == 0 || str(data, "token") == "" {
			return "", 0, errors.New("kucoin websocket configuration unavailable")
		}
		server := rec(servers[0])
		endpoint = str(server, "endpoint") + "?token=" + url.QueryEscape(str(data, "token")) + "&connectId=" + fmt.Sprint(time.Now().UnixNano())
		interval := time.Duration(number(server, "pingInterval")) * time.Millisecond
		if interval < time.Second {
			interval = 15 * time.Second
		}
		return endpoint, interval / 2, nil
	default:
		return "", 0, errors.New("unsupported exchange")
	}
	key := strings.ToUpper(c.Exchange) + "_SPOT_WS_URL"
	if kind == "perpetual" {
		key = strings.ToUpper(c.Exchange) + "_FUTURES_WS_URL"
	}
	return env(key, endpoint), 15 * time.Second, nil
}
func (c *Client) Stream(ctx context.Context, kind string, list []market.Instrument, onBook func(market.Book), onConnect func(bool)) error {
	endpoint, pingInterval, err := c.websocketURL(ctx, kind)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 15 * time.Second}
	headers := http.Header{}
	if c.Exchange == "gateio" {
		headers.Set("X-Gate-Size-Decimal", "1")
	}
	conn, resp, err := dialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("%s %s websocket connection failed", c.Exchange, kind)
	}
	defer conn.Close()
	conn.SetReadLimit(4 << 20)
	var mu sync.Mutex
	write := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		if err := conn.SetWriteDeadline(time.Now().Add(8 * time.Second)); err != nil {
			return err
		}
		return conn.WriteJSON(v)
	}
	go func() { <-ctx.Done(); conn.Close() }()
	defer onConnect(false)
	byNative := map[string]market.Instrument{}
	for _, i := range list {
		byNative[i.Native] = i
	}
	for start := 0; start < len(list); start += 10 {
		end := min(start+10, len(list))
		chunk := list[start:end]
		switch c.Exchange {
		case "binance":
			args := []string{}
			for _, i := range chunk {
				args = append(args, strings.ToLower(i.Native)+"@depth5@100ms")
			}
			err = write(map[string]any{"method": "SUBSCRIBE", "params": args, "id": start + 1})
		case "bybit":
			args := []string{}
			for _, i := range chunk {
				args = append(args, "orderbook.1."+i.Native)
			}
			err = write(map[string]any{"op": "subscribe", "args": args})
		case "okx", "bitget":
			args := []map[string]string{}
			for _, i := range chunk {
				arg := map[string]string{"channel": "books5", "instId": i.Native}
				if c.Exchange == "bitget" {
					arg["instType"] = "SPOT"
					if kind == "perpetual" {
						arg["instType"] = "USDT-FUTURES"
					}
				}
				args = append(args, arg)
			}
			err = write(map[string]any{"op": "subscribe", "args": args})
		case "gateio":
			channel := "spot.order_book"
			if kind == "perpetual" {
				channel = "futures.order_book"
			}
			for _, i := range chunk {
				interval := "100ms"
				if kind == "perpetual" {
					interval = "0"
				}
				err = write(map[string]any{"time": time.Now().Unix(), "channel": channel, "event": "subscribe", "payload": []string{i.Native, "5", interval}})
				if err != nil {
					break
				}
			}
		case "kucoin":
			prefix := "/spotMarket/level2Depth5:"
			if kind == "perpetual" {
				prefix = "/contractMarket/level2Depth5:"
			}
			for _, i := range chunk {
				err = write(map[string]any{"id": fmt.Sprint(time.Now().UnixNano()), "type": "subscribe", "topic": prefix + i.Native, "response": true})
				if err != nil {
					break
				}
			}
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	onConnect(true)
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// A cancelled connection must stop ping goroutines before reconnecting.
	go func() {
		t := time.NewTicker(pingInterval)
		defer t.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-t.C:
				var pingErr error
				mu.Lock()
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				switch c.Exchange {
				case "bybit":
					pingErr = conn.WriteJSON(map[string]string{"op": "ping"})
				case "okx", "bitget":
					pingErr = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
				case "kucoin":
					pingErr = conn.WriteJSON(map[string]string{"id": fmt.Sprint(time.Now().UnixNano()), "type": "ping"})
				default:
					pingErr = conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second))
				}
				mu.Unlock()
				if pingErr != nil {
					conn.Close()
					return
				}
			}
		}
	}()
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	for {
		_, body, e := conn.ReadMessage()
		if e != nil {
			return e
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if string(body) == "pong" {
			continue
		}
		b, ok, e := DecodeBook(c.Exchange, kind, body, byNative, time.Now())
		if e != nil {
			return e
		}
		if ok {
			onBook(b)
		}
	}
}
