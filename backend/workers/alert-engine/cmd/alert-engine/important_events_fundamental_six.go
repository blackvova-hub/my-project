package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var officialStatementSources = map[string]struct{}{
	"whitehouse": {}, "federal_reserve": {}, "sec": {}, "federal_register": {},
}

var statementActors = []string{
	"donald trump", "president trump", "u.s. president", "us president", "the president",
	"white house", "jerome powell", "chair powell", "federal reserve chair", "fed chair",
	"fomc member", "federal reserve governor", "sec chair", "sec chairman", "sec commissioner",
	"президент сша", "дональд трамп", "белый дом", "джером пауэлл", "председатель фрс",
	"член fomc", "член фрс", "председатель sec", "комиссар sec",
}

var marketStatementTerms = []string{
	"crypto", "cryptocurrency", "cryptocurrencies", "digital asset", "digital assets", "virtual asset",
	"bitcoin", "btc", "ethereum", "ether", "eth", "solana", "xrp", "dogecoin", "stablecoin", "stablecoins",
	"bnb", "cardano", "$ada", "tron", "trx", "chainlink", "$link", "toncoin", "avalanche", "avax",
	"polkadot", "$dot", "litecoin", "ltc", "bitcoin cash", "bch", "sui", "near protocol", "aptos",
	"memecoin", "meme coin", "altcoin", "real world asset", "rwa", "non-fungible token", "nft",
	"tokenization", "tokenized", "blockchain", "distributed ledger", "defi", "decentralized finance",
	"central bank digital currency", "cbdc", "strategic bitcoin reserve", "digital asset reserve",
	"crypto reserve", "bitcoin reserve", "crypto exchange", "digital asset exchange", "crypto custody",
	"self-custody", "wallet provider", "proof of reserves", "mining", "staking", "staking services",
	"crypto wallet", "hardware wallet", "cross-chain bridge", "crypto mixer", "token sale", "initial coin offering",
	"crypto hack", "blockchain exploit", "ransomware payment", "exchange reserves", "reserve attestation",
	"spot bitcoin etf", "spot ether etf", "bitcoin etf", "ethereum etf", "crypto etf", "etf approval",
	"exchange-traded fund", "exchange traded fund", "etf redemption", "etf creation",
	"securities law", "securities regulation", "securities enforcement", "investment contract",
	"broker-dealer", "broker dealer", "investment adviser", "custody rule", "market structure",
	"rulemaking", "proposed rule", "final rule", "regulatory guidance", "no-action letter", "registration requirement",
	"licensing requirement", "licence requirement", "approval order", "disapproval order", "trading prohibition",
	"commodity futures", "cftc", "sec enforcement", "wells notice", "consent order", "cease-and-desist",
	"civil penalty", "criminal charges", "indictment", "money laundering", "anti-money laundering",
	"aml", "know your customer", "kyc", "bank secrecy act", "financial crime", "asset freeze",
	"sanction", "sanctions", "sanctioned", "ofac", "special designated nationals", "sdn list",
	"export controls", "capital controls", "banking restriction", "banking restrictions",
	"interest rate", "interest rates", "rate cut", "rate cuts", "rate hike", "rate hikes",
	"federal funds rate", "policy rate", "discount rate", "monetary policy", "quantitative easing",
	"quantitative tightening", "balance sheet runoff", "treasury purchases", "bond purchases",
	"reserve requirement", "dot plot", "fomc minutes", "forward guidance", "credit conditions", "bank lending",
	"liquidity facility", "repo facility", "reverse repo", "yield curve", "treasury yields",
	"inflation", "consumer price index", "cpi", "producer price index", "ppi", "pce inflation",
	"core pce", "employment situation", "nonfarm payroll", "non-farm payroll", "unemployment rate",
	"jobless claims", "gross domestic product", "gdp", "recession", "financial stability",
	"systemic risk", "bank failure", "banking crisis", "deposit insurance", "fdic", "capital requirement",
	"tariff", "tariffs", "trade war", "trade restriction", "import duty", "export ban",
	"debt ceiling", "government shutdown", "sovereign default", "treasury market", "currency intervention",
	"foreign exchange intervention", "dollar liquidity", "reserve currency", "fiscal stimulus",
	"финансовые рынки", "денежно-кредитная политика", "процентная ставка", "снижение ставки",
	"повышение ставки", "ключевая ставка", "санкции", "тарифы", "торговая война", "биткоин",
	"криптовалюта", "цифровые активы", "стейблкоин", "блокчейн", "криптобиржа", "регулирование бирж",
	"биржевой фонд", "спотовый etf", "банковские ограничения", "резерв биткоина", "крипторезерв",
	"токенизация", "децентрализованные финансы", "криптокошелек", "кастодиальные услуги", "майнинг",
	"стейкинг", "листинг", "делистинг", "приостановка торгов", "правило sec", "правоприменение sec",
	"денежная масса", "количественное смягчение", "количественное ужесточение", "баланс фрс",
	"индекс потребительских цен", "индекс цен производителей", "занятость", "безработица", "госдолг",
	"потолок долга", "дефолт", "банковский кризис", "контроль капитала", "экспортные ограничения",
}

func readSixFundamentalImportantEvents(ctx context.Context, db *pgxpool.Pool) ([]importantEvent, error) {
	rows, err := db.Query(ctx, `SELECT n.id::text,COALESCE(n.source_id,''),COALESCE(n.source_type,''),COALESCE(n.event_type,''),
		COALESCE(NULLIF(n.translated_title,''),n.title),COALESCE(NULLIF(n.translated_summary,''),n.summary,''),
		COALESCE(n.original_title,''),COALESCE(n.original_text,''),n.published_at,n.event_at,
		COALESCE(n.entities,'[]'::jsonb),COALESCE(n.assets,'[]'::jsonb),n.url
		FROM news_items n
		WHERE n.published_at >= now()-interval '48 hours'
		  AND n.publication_eligible = TRUE
		  AND NOT EXISTS (SELECT 1 FROM important_events e WHERE e.catalyst_event_id=n.id)
		ORDER BY n.published_at ASC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	result := make([]importantEvent, 0, 16)
	for rows.Next() {
		var id, sourceID, sourceType, subtype, title, summary, originalTitle, originalText, url string
		var published time.Time
		var eventAt *time.Time
		var entitiesJSON, assetsJSON []byte
		if err := rows.Scan(&id, &sourceID, &sourceType, &subtype, &title, &summary, &originalTitle, &originalText, &published, &eventAt, &entitiesJSON, &assetsJSON, &url); err != nil {
			return nil, err
		}
		var entities, assets []string
		_ = json.Unmarshal(entitiesJSON, &entities)
		_ = json.Unmarshal(assetsJSON, &assets)
		sourceID = strings.ToLower(strings.TrimSpace(sourceID))
		sourceType = strings.ToLower(strings.TrimSpace(sourceType))
		subtype = strings.ToLower(strings.TrimSpace(subtype))
		if isExchangeAnnouncementSubtype(subtype) {
			if eventAt == nil || eventAt.Before(now) || eventAt.After(now.Add(7*24*time.Hour)) ||
				sourceType != "official_exchange" || !isOfficialExchangeID(sourceID) || len(canonicalAssets(assets)) == 0 {
				continue
			}
			canonical := canonicalAssets(assets)
			event := importantEvent{
				EventType: "exchange_announcement", Family: "exchange_announcement",
				Confidence: 3,
				Title:      title, Details: summary, Exchange: sourceID, MarketType: announcementMarketType(subtype),
				Symbol: canonical[0], Direction: announcementDirection(subtype), WindowMinutes: 1,
				EventAt: eventAt.UTC(), SourceKind: "news", SourceRef: id, CatalystEventID: id,
				Metadata: map[string]any{
					"announcementType": subtype, "assets": canonical, "entities": entities, "url": url,
					"sourceId": sourceID, "sourceType": sourceType, "publishedAt": published.UTC().Format(time.RFC3339),
					"requiredObservations": 1, "clusterWindowMinutes": 1440,
				},
			}
			result = append(result, event)
			continue
		}
		text := strings.ToLower(strings.Join([]string{title, summary, originalTitle, originalText}, " "))
		topic := statementMarketTopic(text)
		if topic == "" {
			continue
		}
		_, officialID := officialStatementSources[sourceID]
		official := officialID && sourceType == "official_government"
		actor := statementActor(text, sourceID)
		if !official && actor == "" {
			continue
		}
		symbol := "MARKET"
		if values := canonicalAssets(assets); len(values) > 0 {
			symbol = values[0]
		}
		required, confirmationMinutes, confidence := 1, 48*60, 1
		if official {
			required, confirmationMinutes, confidence = 1, 1, 3
		}
		fingerprintActor := actor
		if fingerprintActor == "" {
			fingerprintActor = sourceID
		}
		event := importantEvent{
			EventType: "major_statement", Family: "major_statement", Confidence: confidence,
			Title: title, Details: summary,
			Symbol: symbol, Direction: "statement", WindowMinutes: 1, EventAt: published.UTC(),
			SourceKind: "news", SourceRef: id, CatalystEventID: id,
			Metadata: map[string]any{
				"statementTopic": topic, "actor": fingerprintActor, "official": official, "url": url,
				"requiresOfficialConfirmation": !official,
				"sourceId":                     sourceID, "sourceType": sourceType, "assets": canonicalAssets(assets), "entities": entities,
				"identityFingerprint":  majorStatementFingerprint(fingerprintActor, topic, published),
				"requiredObservations": required, "confirmationWindowMinutes": confirmationMinutes,
				"clusterWindowMinutes": 24 * 60,
			},
		}
		result = append(result, event)
	}
	return result, rows.Err()
}

func isOfficialExchangeID(id string) bool {
	switch id {
	case "binance", "bybit", "coinbase", "upbit", "bithumb":
		return true
	default:
		return false
	}
}

func canonicalAssets(assets []string) []string {
	out := make([]string, 0, len(assets))
	for _, asset := range assets {
		asset = strings.ToUpper(strings.TrimSpace(asset))
		if parts := strings.FieldsFunc(asset, func(r rune) bool { return r == '-' || r == '/' || r == '_' }); len(parts) > 1 {
			asset = parts[0]
		}
		for _, quote := range []string{"USDT", "USDC", "USD", "KRW"} {
			if strings.HasSuffix(asset, quote) && len(asset) > len(quote) {
				asset = strings.TrimSuffix(asset, quote)
				break
			}
		}
		if asset != "" {
			out = append(out, asset)
		}
	}
	return uniqueStrings(out)
}

func isExchangeAnnouncementSubtype(value string) bool {
	switch value {
	case "spot_listing", "futures_listing", "delisting", "trading_suspension":
		return true
	default:
		return false
	}
}

func announcementMarketType(subtype string) string {
	if subtype == "futures_listing" {
		return "futures"
	}
	return "spot"
}

func announcementDirection(subtype string) string {
	if subtype == "spot_listing" || subtype == "futures_listing" {
		return "listing"
	}
	if subtype == "delisting" {
		return "delisting"
	}
	return "suspension"
}

func statementActor(text, sourceID string) string {
	if sourceID == "whitehouse" {
		return "white_house"
	}
	if sourceID == "federal_reserve" {
		return "federal_reserve"
	}
	if sourceID == "sec" {
		return "sec"
	}
	if sourceID == "federal_register" {
		return "us_executive_branch"
	}
	for _, actor := range statementActors {
		if strings.Contains(text, actor) {
			switch {
			case strings.Contains(actor, "trump"), strings.Contains(actor, "president"), strings.Contains(actor, "white house"),
				strings.Contains(actor, "президент"), strings.Contains(actor, "трамп"), strings.Contains(actor, "белый дом"):
				return "white_house"
			case strings.Contains(actor, "powell"), strings.Contains(actor, "federal reserve"), strings.Contains(actor, "fed "),
				strings.Contains(actor, "fomc"), strings.Contains(actor, "пауэлл"), strings.Contains(actor, "фрс"):
				return "federal_reserve"
			case strings.Contains(actor, "sec"):
				return "sec"
			default:
				return strings.ReplaceAll(actor, " ", "_")
			}
		}
	}
	return ""
}

func statementMarketTopic(text string) string {
	for _, term := range marketStatementTerms {
		if containsStatementTerm(text, term) {
			switch {
			case containsAnyFold(term, "crypto", "bitcoin", "ethereum", "stablecoin", "blockchain", "digital asset", "token", "defi", "крипто", "биткоин", "стейблкоин"):
				return "crypto_digital_assets"
			case containsAnyFold(term, "interest", "monetary", "inflation", "cpi", "ppi", "pce", "employment", "payroll", "gdp", "rate", "лик", "ставк"):
				return "monetary_macro"
			case containsAnyFold(term, "sanction", "tariff", "trade war", "export", "capital control", "санкц", "тариф"):
				return "sanctions_trade"
			case containsAnyFold(term, "securit", "enforcement", "regulation", "broker", "custody", "money laundering", "ofac", "регулир"):
				return "financial_regulation"
			default:
				return "financial_markets"
			}
		}
	}
	return ""
}

func containsStatementTerm(text, term string) bool {
	if strings.ContainsAny(term, " -_") || !asciiStatementWord(term) {
		return strings.Contains(text, term)
	}
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], term)
		if index < 0 {
			return false
		}
		index += start
		beforeOK := index == 0 || !asciiStatementWordByte(text[index-1])
		after := index + len(term)
		afterOK := after == len(text) || !asciiStatementWordByte(text[after])
		if beforeOK && afterOK {
			return true
		}
		start = index + 1
	}
	return false
}

func asciiStatementWord(value string) bool {
	if value == "" {
		return false
	}
	for index := range value {
		if !asciiStatementWordByte(value[index]) {
			return false
		}
	}
	return true
}

func asciiStatementWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9'
}

func containsAnyFold(value string, terms ...string) bool {
	value = strings.ToLower(value)
	for _, term := range terms {
		if strings.Contains(value, term) {
			return true
		}
	}
	return false
}

func majorStatementFingerprint(actor, topic string, published time.Time) string {
	return fmt.Sprintf("%s:%s:%s", actor, topic, published.UTC().Format("2006-01-02"))
}
