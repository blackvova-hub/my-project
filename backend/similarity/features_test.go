package similarity

import (
	"math"
	bt "shortlong/backtest"
	"testing"
	"time"
)

func fixture(n int) []bt.Candle {
	out := make([]bt.Candle, n)
	p := 100.
	for i := range out {
		open := p
		p *= 1 + .001*math.Sin(float64(i)*.31) + .0002
		out[i] = bt.Candle{Time: 1740000000000 + int64(i)*Step, Open: open, Close: p, High: math.Max(open, p) * 1.002, Low: math.Min(open, p) * .998, Volume: 100 + 30*math.Sin(float64(i)*.6)}
	}
	return out
}
func TestScaleInvarianceAndRanking(t *testing.T) {
	a := fixture(576)
	sig, e := Features(a, 288)
	if e != nil {
		t.Fatal(e)
	}
	b := append([]bt.Candle(nil), a...)
	for i := range b {
		b[i].Open *= 700
		b[i].High *= 700
		b[i].Low *= 700
		b[i].Close *= 700
		b[i].Volume *= .003
	}
	other, e := Features(b, 288)
	if e != nil {
		t.Fatal(e)
	}
	score, _ := Compare(sig, other, DefaultWeights)
	if math.Abs(score-100) > 1e-7 {
		t.Fatalf("absolute price/volume leaked: %f", score)
	}
	vec := sig.Vector(DefaultWeights)
	if len(vec) != Dimensions {
		t.Fatal(len(vec))
	}
	restored, e := FromVector(vec, DefaultWeights)
	if e != nil {
		t.Fatal(e)
	}
	score, _ = Compare(sig, restored, DefaultWeights)
	if math.Abs(score-100) > 1e-7 {
		t.Fatal(score)
	}
	for _, v := range vec {
		if math.IsInf(v, 0) || math.IsNaN(v) {
			t.Fatal("nonfinite signature")
		}
	}
}
func TestMissingBarsAndFlatMarket(t *testing.T) {
	a := fixture(24)
	a[8].Time += Step
	if _, e := Features(a, 12); e == nil {
		t.Fatal("gap accepted")
	}
	a = fixture(24)
	for i := range a {
		a[i].Open = 1
		a[i].High = 1
		a[i].Low = 1
		a[i].Close = 1
		a[i].Volume = 0
	}
	s, e := Features(a, 12)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range s.Vector(DefaultWeights) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatal(v)
		}
	}
}
func TestFutureNoLookahead(t *testing.T) {
	a := fixture(25)
	end := a[0].Time + 12*Step
	o, e := Future(a[11:24], end, 12)
	if e != nil {
		t.Fatal(e)
	}
	want := (a[23].Close/a[11].Close - 1) * 100
	if math.Abs(o.Return-want) > 1e-9 || o.AvailableAt != end+12*Step {
		t.Fatal(o)
	}
	if _, e = Future(a[11:23], end, 12); e == nil {
		t.Fatal("immature future accepted")
	}
	s1, _ := Features(a[:24], 12)
	a[24].Close *= 100
	s2, _ := Features(a[:24], 12)
	score, _ := Compare(s1, s2, DefaultWeights)
	if score != 100 {
		t.Fatal("future affected features")
	}
}
func TestScopesDedupAndValidation(t *testing.T) {
	assets := []Asset{{Market: "linear", Symbol: "BTCUSDT", Enabled: true}, {Market: "linear", Symbol: "ETHUSDT", Alt: true, Enabled: true, Sectors: []string{"l1"}}, {Market: "linear", Symbol: "USDCUSDT", Stable: true, Enabled: true}, {Market: "spot", Symbol: "SOLUSDT", Enabled: true}}
	r := Request{Market: "linear", Symbol: "BTCUSDT", Scope: "alts", Window: 12, Limit: 6}
	if a := Allowed(assets, r); len(a) != 1 || a[0] != "ETHUSDT" {
		t.Fatal(a)
	}
	r.Scope = "sector"
	r.Sector = "l1"
	if len(Allowed(assets, r)) != 1 {
		t.Fatal("sector")
	}
	r.Scope = "same_asset"
	r.Sector = ""
	duration := int64(12) * Step
	m := []Match{{ID: "1", Symbol: "BTCUSDT", Start: 0, End: duration, Score: 90}, {ID: "2", Symbol: "BTCUSDT", Start: Step, End: duration + Step, Score: 95}, {ID: "3", Symbol: "BTCUSDT", Start: duration + Step, End: 2*duration + Step, Score: 80}}
	dedup := Deduplicate(m, r)
	if len(dedup) != 2 || dedup[0].ID != "2" {
		t.Fatal(dedup)
	}
	t.Setenv("SIMILARITY_WINDOWS", "")
	c, e := ConfigFromEnv()
	if e != nil {
		t.Fatal(e)
	}
	if e = r.Validate(c, time.Now()); e != nil {
		t.Fatal(e)
	}
	r.End += Step
	if e = r.Validate(c, time.Now()); e == nil {
		t.Fatal("future query allowed")
	}
}
func TestChartPreservesBoundary(t *testing.T) {
	c := fixture(24)
	end := c[11].Time
	bars := ChartBars(c, 5, end)
	found := false
	for _, b := range bars {
		if b.Time*1000 == end {
			found = true
		}
	}
	if !found {
		t.Fatal("future boundary blended into matching bar")
	}
}

func TestTinyAssetAndMissingVolumeReference(t *testing.T) {
	for _, zeroPrior := range []bool{false, true} {
		a := fixture(576)
		if zeroPrior {
			for i := 0; i < 288; i++ {
				a[i].Volume = 0
			}
		}
		s, e := Features(a, 288)
		if e != nil {
			t.Fatal(e)
		}
		b := append([]bt.Candle(nil), a...)
		for i := range b {
			b[i].Open *= 1e-12
			b[i].High *= 1e-12
			b[i].Low *= 1e-12
			b[i].Close *= 1e-12
			b[i].Volume *= 1e-18
		}
		other, e := Features(b, 288)
		if e != nil {
			t.Fatal(e)
		}
		score, _ := Compare(s, other, DefaultWeights)
		if math.Abs(score-100) > 1e-7 {
			t.Fatalf("scale-dependent score for zeroPrior=%v: %f", zeroPrior, score)
		}
	}
}
