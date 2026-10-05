package similarity

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Window struct {
	Bars   int `json:"bars"`
	Stride int `json:"stride"`
}
type Config struct {
	Windows     []Window
	Weights     [5]float64
	Candidates  int
	Concurrency int
	Version     string
	HotSymbols  []string
}

func ConfigFromEnv() (Config, error) {
	c := Config{Weights: DefaultWeights, Candidates: 500, Concurrency: 2, HotSymbols: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}}
	if raw := os.Getenv("SIMILARITY_HOT_SYMBOLS"); raw != "" {
		c.HotSymbols = strings.Split(raw, ",")
		for i, s := range c.HotSymbols {
			c.HotSymbols[i] = strings.TrimSpace(strings.ToUpper(s))
		}
	}
	spec := os.Getenv("SIMILARITY_WINDOWS")
	if spec == "" {
		spec = "12:3,36:6,72:12,144:12,288:24,864:72,2016:144"
	}
	seen := map[int]bool{}
	for _, item := range strings.Split(spec, ",") {
		p := strings.Split(item, ":")
		if len(p) != 2 {
			return c, fmt.Errorf("windows must be bars:stride")
		}
		n, e := strconv.Atoi(p[0])
		stride, se := strconv.Atoi(p[1])
		if e != nil || se != nil || n < 12 || n > 2016 || stride < 1 || stride > n || seen[n] {
			return c, fmt.Errorf("invalid window %q", item)
		}
		seen[n] = true
		c.Windows = append(c.Windows, Window{n, stride})
	}
	if len(c.Windows) > 12 {
		return c, fmt.Errorf("too many window sizes")
	}
	if v := os.Getenv("SIMILARITY_WEIGHTS"); v != "" {
		var w []float64
		if err := json.Unmarshal([]byte(v), &w); err != nil || len(w) != 5 {
			return c, fmt.Errorf("five weights required")
		}
		sum := 0.
		for i, x := range w {
			if x <= 0 || x > 1 {
				return c, fmt.Errorf("weights must be positive, <=1")
			}
			c.Weights[i] = x
			sum += x
		}
		for i := range c.Weights {
			c.Weights[i] /= sum
		}
	}
	for key, dst := range map[string]*int{"SIMILARITY_CANDIDATES": &c.Candidates, "SIMILARITY_CONCURRENCY": &c.Concurrency} {
		if v := os.Getenv(key); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil {
				return c, e
			}
			*dst = n
		}
	}
	if c.Candidates < 200 || c.Candidates > 1000 || c.Concurrency < 1 || c.Concurrency > 4 {
		return c, fmt.Errorf("candidates 200..1000, concurrency 1..4")
	}
	raw, _ := json.Marshal(c.Weights)
	h := sha256.Sum256(raw)
	c.Version = fmt.Sprintf("%s_%x", FeatureVersion, h[:6])
	return c, nil
}
func (c Config) Window(n int) (Window, bool) {
	for _, w := range c.Windows {
		if w.Bars == n {
			return w, true
		}
	}
	return Window{}, false
}
func (c Config) OutcomeStride() int {
	g := c.Windows[0].Stride
	for _, w := range c.Windows {
		a, b := g, w.Stride
		for b != 0 {
			a, b = b, a%b
		}
		g = a
	}
	return g
}

var Horizons = []int{12, 36, 72, 144, 288, 864, 2016}
