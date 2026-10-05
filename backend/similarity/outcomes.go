package similarity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"shortlong/backtest/marketdata"
	"strconv"
)

type OutcomeRow struct {
	Market string `json:"market"`
	Symbol string `json:"symbol"`
	End    int64  `json:"end"`
	Outcome
}
type Outcomes struct{ Storage *marketdata.ClickHouse }

func (o Outcomes) query(ctx context.Context, sql string, params url.Values, body []byte) ([]byte, error) {
	c := o.Storage
	u, err := url.Parse(c.URL)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("query", sql)
	q.Set("wait_end_of_query", "1")
	q.Set("output_format_json_quote_64bit_integers", "0")
	for k, v := range params {
		q[k] = v
	}
	u.RawQuery = q.Encode()
	r, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.SetBasicAuth(c.User, c.Password)
	res, err := c.HTTP.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("outcomes storage HTTP %d: %.200s", res.StatusCode, raw)
	}
	return raw, nil
}
func (o Outcomes) Init(ctx context.Context) error {
	_, e := o.query(ctx, `CREATE TABLE IF NOT EXISTS shortlong_backtest.similarity_outcomes_v1(market LowCardinality(String),symbol LowCardinality(String),end Int64,horizonBars UInt16,returnPct Float64,maxUpsidePct Float64,maxDownsidePct Float64,availableAt Int64,version UInt64 DEFAULT toUnixTimestamp64Milli(now64(3))) ENGINE=ReplacingMergeTree(version) PARTITION BY toYYYYMM(fromUnixTimestamp64Milli(end)) ORDER BY(market,symbol,end,horizonBars)`, nil, nil)
	return e
}
func (o Outcomes) Insert(ctx context.Context, rows []OutcomeRow) error {
	if len(rows) == 0 {
		return nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, r := range rows {
		if e := enc.Encode(r); e != nil {
			return e
		}
	}
	_, e := o.query(ctx, `INSERT INTO shortlong_backtest.similarity_outcomes_v1(market,symbol,end,horizonBars,returnPct,maxUpsidePct,maxDownsidePct,availableAt) FORMAT JSONEachRow`, nil, b.Bytes())
	return e
}
func (o Outcomes) Read(ctx context.Context, market, symbol string, end, asOf int64) ([]Outcome, error) {
	p := url.Values{"param_market": {market}, "param_symbol": {symbol}, "param_end": {strconv.FormatInt(end, 10)}, "param_asof": {strconv.FormatInt(asOf, 10)}}
	raw, e := o.query(ctx, `SELECT horizonBars,returnPct,maxUpsidePct,maxDownsidePct,availableAt FROM shortlong_backtest.similarity_outcomes_v1 FINAL WHERE market={market:String} AND symbol={symbol:String} AND end={end:Int64} AND availableAt<={asof:Int64} ORDER BY horizonBars FORMAT JSONEachRow`, p, nil)
	if e != nil {
		return nil, e
	}
	out := []Outcome{}
	d := json.NewDecoder(bytes.NewReader(raw))
	for {
		var r Outcome
		if e = d.Decode(&r); e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}
