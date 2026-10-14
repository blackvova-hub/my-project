package macroapi

type Point struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type Series struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	Country         string   `json:"country"`
	Currency        string   `json:"currency"`
	Category        string   `json:"category"`
	Unit            string   `json:"unit"`
	Frequency       string   `json:"frequency"`
	Source          string   `json:"source"`
	SourceURL       string   `json:"sourceUrl"`
	ObservationDate *string  `json:"observationDate"`
	Latest          *float64 `json:"latest"`
	Previous        *float64 `json:"previous"`
	Change          *float64 `json:"change"`
	Points          []Point  `json:"points"`
	Status          string   `json:"status"`
	LastUpdated     *string  `json:"lastUpdated"`
	RevisionNote    string   `json:"revisionNote"`
	Explanation     string   `json:"explanation"`
	Scenarios       []string `json:"scenarios"`
}

type Event struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	Country             string   `json:"country"`
	Currency            string   `json:"currency"`
	Category            string   `json:"category"`
	Importance          string   `json:"importance"`
	ImportanceReason    string   `json:"importanceReason"`
	ScheduledAt         *string  `json:"scheduledAt"`
	Date                string   `json:"date"`
	TimePrecision       string   `json:"timePrecision"`
	Status              string   `json:"status"`
	Actual              *float64 `json:"actual"`
	Forecast            *float64 `json:"forecast"`
	Previous            *float64 `json:"previous"`
	Unit                string   `json:"unit"`
	SeriesID            *string  `json:"seriesId"`
	Source              string   `json:"source"`
	SourceURL           string   `json:"sourceUrl"`
	ReferencePeriod     *string  `json:"referencePeriod"`
	PreviousScheduledAt *string  `json:"previousScheduledAt"`
}

type SourceHealth struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	URL         string  `json:"url"`
	Status      string  `json:"status"`
	LastSuccess *string `json:"lastSuccess"`
	LastAttempt *string `json:"lastAttempt"`
	Message     string  `json:"message"`
}

type MarketItem struct {
	Symbol         string   `json:"symbol"`
	Name           string   `json:"name"`
	AssetClass     string   `json:"assetClass"`
	Price          *float64 `json:"price"`
	ChangePercent  *float64 `json:"changePercent"`
	Unit           string   `json:"unit"`
	Source         string   `json:"source"`
	SourceURL      string   `json:"sourceUrl"`
	ObservedAt     *string  `json:"observedAt"`
	Points         []Point  `json:"points"`
	Status         string   `json:"status"`
	Note           string   `json:"note"`
	Classification *string  `json:"classification,omitempty"`
}

type snapshot struct {
	Series    map[string]Series       `json:"series"`
	Events    []Event                 `json:"events"`
	Markets   []MarketItem            `json:"markets"`
	Sentiment *MarketItem             `json:"sentiment"`
	Sources   map[string]SourceHealth `json:"sources"`
}

func ptr[T any](v T) *T { return &v }
