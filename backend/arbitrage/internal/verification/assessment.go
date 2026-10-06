package verification

import (
	"shortlong/arbitrage/internal/engine"
	"shortlong/arbitrage/internal/market"
	"time"
)

type Check struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	State  string `json:"state"` // pass, fail, unknown, na
	Detail string `json:"detail"`
	At     int64  `json:"at,omitempty"`
}
type Calculation struct {
	Execution             *engine.Execution `json:"execution,omitempty"`
	BuyVWAP               *float64          `json:"buyVwap"`
	SellVWAP              *float64          `json:"sellVwap"`
	BuyQuantity           *float64          `json:"buyQuantity"`
	SellQuantity          *float64          `json:"sellQuantity"`
	GrossUSDT             *float64          `json:"grossUsdt"`
	TradingFeesEstimate   *float64          `json:"tradingFeesEstimate"`
	WithdrawalFeeEstimate *float64          `json:"withdrawalFeeEstimate"`
}
type Path struct {
	ID           string      `json:"id"`
	NetworkLabel string      `json:"networkLabel,omitempty"`
	Mode         string      `json:"mode"`
	Network      string      `json:"network,omitempty"`
	State        string      `json:"state"`
	Checks       []Check     `json:"checks"`
	Calculation  Calculation `json:"calculation"`
	NetProfit    *float64    `json:"netProfit"`
}
type Report struct {
	At     int64                `json:"at"`
	Amount float64              `json:"amount"`
	Buy    market.VenueEvidence `json:"buy"`
	Sell   market.VenueEvidence `json:"sell"`
	Paths  []Path               `json:"paths"`
}

func ptr(v float64) *float64 { return &v }
func recent(at int64, now time.Time) bool {
	return at > 0 && at <= now.UnixMilli()+2000 && now.UnixMilli()-at <= 60000
}
func check(key, label, state, detail string, at int64) Check {
	return Check{key, label, state, detail, at}
}
func contractEqual(a, b market.Chain, asset string) bool {
	if a.ID == "" || a.ID != b.ID {
		return false
	}
	if a.Contract != "" && b.Contract != "" {
		return a.Contract == b.Contract
	}
	// Empty contract addresses prove identity only for reviewed native assets.
	return a.Contract == "" && b.Contract == "" && ((asset == "BTC" && a.ID == "bitcoin:mainnet") || (asset == "ETH" && a.ID == "eip155:1"))
}
func identity(b, s market.Book, be, se market.VenueEvidence, now time.Time) Check {
	c := check("identity", "Один и тот же актив", "unknown", "Тикера недостаточно: нужны совпадающие сеть и контракт либо проверенный native-актив", 0)
	if b.Instrument.Identity() != s.Instrument.Identity() {
		c.State = "fail"
		c.Detail = "Разные идентичности активов"
		return c
	}
	if !recent(be.Wallet.At, now) || !recent(se.Wallet.At, now) {
		return c
	}
	mismatch := false
	for _, a := range be.Wallet.Chains {
		for _, z := range se.Wallet.Chains {
			if contractEqual(a, z, b.Instrument.Asset) {
				c.State = "pass"
				c.Detail = "Подтверждено публичными метаданными: " + a.ID
				c.At = min(be.Wallet.At, se.Wallet.At)
				return c
			}
			if a.ID != "" && a.ID == z.ID && a.Contract != "" && z.Contract != "" && a.Contract != z.Contract {
				mismatch = true
			}
		}
	}
	if mismatch {
		c.State = "fail"
		c.Detail = "На общей сети контракты различаются; связка отклонена"
	}
	return c
}
func Assess(row engine.Opportunity, b, s market.Book, be, se market.VenueEvidence, amount float64, now time.Time) Report {
	r := Report{At: now.UnixMilli(), Amount: amount, Buy: be, Sell: se, Paths: []Path{}}
	base := []Check{identity(b, s, be, se, now)}
	fresh := check("freshness", "Свежие цены", "fail", "Нет двух валидных котировок не старше 5 секунд", min(b.QuoteAt(), s.QuoteAt()))
	if b.Fresh(now, 5*time.Second) && s.Fresh(now, 5*time.Second) {
		fresh.State = "pass"
		fresh.Detail = "Обе котировки не старше 5 секунд"
	}
	base = append(base, fresh)
	trade := check("trading", "Торговля на биржах", "unknown", "Нет свежего подтверждения двух рынков из каталогов", 0)
	if recent(be.CatalogAt, now) && recent(se.CatalogAt, now) && be.Trading != nil && se.Trading != nil {
		trade.At = min(be.CatalogAt, se.CatalogAt)
		trade.State = "pass"
		trade.Detail = "Оба рынка активны в публичных каталогах; допуск аккаунта проверяется отдельно"
		if !*be.Trading || !*se.Trading {
			trade.State = "fail"
			trade.Detail = "Один из рынков отсутствует среди активных"
		}
	}
	base = append(base, trade)
	delist := check("delisting", "Нет известного делистинга", "unknown", "Каталог не подтверждает отсутствие объявленного делистинга", 0)
	if be.Instrument != nil && se.Instrument != nil && recent(be.CatalogAt, now) && recent(se.CatalogAt, now) {
		if be.Instrument.DelistingAt > 0 || se.Instrument.DelistingAt > 0 {
			delist.State = "fail"
			delist.Detail = "Биржа указала дату прекращения торгов"
		} else if be.Instrument.DelistingKnown && se.Instrument.DelistingKnown {
			delist.State = "pass"
			delist.Detail = "В проверенных полях каталогов делистинг не указан"
		}
		delist.At = min(be.CatalogAt, se.CatalogAt)
	}
	spot := b.Instrument.Market == "spot" && s.Instrument.Market == "spot"
	makePath := func(mode string, a, z *market.Chain) Path {
		p := Path{ID: mode, Mode: mode, State: "incomplete", Checks: append([]Check{}, base...), NetProfit: nil}
		transfer := mode == "transfer"
		io := check("wallet", "Вывод → ввод", "na", "Для одновременных сделок перевод монеты не требуется", 0)
		network := check("network", "Одна сеть и контракт", "na", "Средства заранее размещены на двух биржах", 0)
		if transfer {
			io.State = "unknown"
			io.Detail = "Нет свежих данных о выводе у покупателя и вводе у продавца"
			network.State = "unknown"
			network.Detail = "Общая совместимая сеть не подтверждена"
			if recent(be.Wallet.At, now) && recent(se.Wallet.At, now) && a != nil && z != nil {
				p.Network = a.ID
				p.ID = "transfer:" + a.ID + ":" + a.Name + ":" + z.Name
				p.NetworkLabel = a.Name + " → " + z.Name
				io.At = min(be.Wallet.At, se.Wallet.At)
				network.At = io.At
				network.State = "pass"
				network.Detail = a.Name + " → " + z.Name + "; актив и сеть совпадают"
				if a.Withdraw != nil && z.Deposit != nil {
					io.State = "pass"
					io.Detail = "Публичные статусы разрешают вывод и ввод; ограничения аккаунта отдельно"
					if !*a.Withdraw || !*z.Deposit {
						io.State = "fail"
						io.Detail = "Вывод или ввод на выбранной сети приостановлен"
					}
				}
			}
		}
		p.Checks = append(p.Checks, io, network, delist)
		depth := check("vwap", "VWAP на заданную сумму", "unknown", "Для этой стратегии нужен условный maker/LP/issuer расчёт, а не две рыночные сделки", 0)
		ordinary := row.Strategy == "spot-spot" || row.Strategy == "cex-cex" || row.Strategy == "spot-perpetual" || row.Strategy == "perpetual-perpetual" || row.Strategy == "funding"
		if ordinary && fresh.State != "pass" {
			depth.Detail = "Стакан устарел или недоступен. VWAP на сумму требует двух свежих котировок"
		}
		if ordinary && fresh.State == "pass" {
			// Current catalog rules supersede the collector's older instrument rules.
			bb, ss := b, s
			if be.Instrument != nil {
				bb.Instrument = *be.Instrument
			}
			if se.Instrument != nil {
				ss.Instrument = *se.Instrument
			}
			buy, sell, qty, ex, ok := engine.SizedExecution(bb, ss, amount)
			depth.State = "fail"
			depth.Detail = "Известной глубины недостаточно для заданной суммы"
			if ok {
				p.Calculation = Calculation{Execution: ex, BuyVWAP: ptr(buy.Price), SellVWAP: ptr(sell.Price), BuyQuantity: ptr(qty), SellQuantity: ptr(qty)}
				depth.At = ex.DepthAt
				if ex.Full && ex.Status == "ready" {
					depth.State = "pass"
					depth.Detail = "Два VWAP на заданную сумму до торговых комиссий; это снимок, не гарантия исполнения"
				}
				if transfer {
					// Without a known withdrawal fee the arriving quantity is unknown.
					p.Calculation.SellVWAP = nil
					p.Calculation.SellQuantity = nil
					if a == nil || z == nil || a.WithdrawFee == nil || a.WithdrawRate == nil || *a.WithdrawRate != 0 {
						if depth.State == "pass" {
							depth.State = "unknown"
							depth.Detail = "Цена покупки рассчитана; объём после неизвестной комиссии вывода не подтверждён"
						}
					} else {
						arrives := qty - *a.WithdrawFee
						arrives = engine.RoundQuantity(arrives, ss.Instrument.Step)
						sp, sok := market.VWAP(s.Bids, arrives)
						p.Calculation.WithdrawalFeeEstimate = ptr(*a.WithdrawFee * buy.Price)
						if sok {
							ex.SellLevels = engine.LevelCount(s.Bids, arrives)
							ex.SellSlippagePct = (1 - sp/s.Bids[0].Price) * 100
							p.Calculation.SellVWAP = ptr(sp)
							p.Calculation.SellQuantity = ptr(arrives)
							if a.MinWithdraw == nil || z.MinDeposit == nil {
								if depth.State == "pass" {
									depth.State = "unknown"
									depth.Detail = "Неизвестен минимум вывода или ввода"
								}
							} else if qty < *a.MinWithdraw || arrives < *z.MinDeposit || arrives < ss.Instrument.MinQuantity || arrives*sp < ss.Instrument.MinNotional {
								depth.State = "fail"
								depth.Detail = "После комиссии вывода сумма ниже минимума сети или ордера"
							}
						} else {
							depth.State = "fail"
							depth.Detail = "После комиссии вывода не осталось допустимого объёма"
						}
					}
				}
				if p.Calculation.SellQuantity != nil && p.Calculation.SellVWAP != nil {
					sellValue := *p.Calculation.SellQuantity * *p.Calculation.SellVWAP
					p.Calculation.TradingFeesEstimate = ptr(buy.Notional*bb.Instrument.TakerFee + sellValue*ss.Instrument.TakerFee)
					if spot {
						p.Calculation.GrossUSDT = ptr(sellValue - buy.Notional)
					}
				}
			}
		}
		p.Checks = append(p.Checks, depth, check("fees", "Все комиссии", "unknown", "Публичные торговые тарифы — оценка. Не подтверждены персональный тариф, валюта списания, комиссии ввода/сети, ребалансировка; для хеджа также закрытие и funding", 0), check("account", "Средства и допуск аккаунта", "unknown", "Баланс, маржа, KYC, регион, лимиты и разрешения аккаунта не подключены", 0))
		if transfer {
			p.Checks[len(p.Checks)-1].Detail += ". Продажа после перевода произойдёт по будущей, неизвестной цене"
		}
		for _, c := range p.Checks {
			if c.State == "fail" {
				p.State = "blocked"
				break
			}
		}
		// Never upgrade estimates to net profit while a required fact is unknown.
		return p
	}
	r.Paths = append(r.Paths, makePath("prefunded", nil, nil))
	if spot {
		if recent(be.Wallet.At, now) && recent(se.Wallet.At, now) {
			for _, a := range be.Wallet.Chains {
				for _, z := range se.Wallet.Chains {
					if contractEqual(a, z, b.Instrument.Asset) {
						r.Paths = append(r.Paths, makePath("transfer", &a, &z))
					}
				}
			}
		}
		if len(r.Paths) == 1 {
			p := makePath("transfer", nil, nil)
			// Complete, fresh, reviewed network lists can also prove incompatibility.
			known := recent(be.Wallet.At, now) && recent(se.Wallet.At, now) && len(be.Wallet.Chains) > 0 && len(se.Wallet.Chains) > 0
			for _, w := range []market.WalletEvidence{be.Wallet, se.Wallet} {
				for _, c := range w.Chains {
					if c.ID == "" {
						known = false
					}
				}
			}
			if known {
				for n, c := range p.Checks {
					if c.Key == "network" {
						p.Checks[n].State = "fail"
						p.Checks[n].Detail = "Нет общей сети с подтверждённым совпадением актива"
					}
				}
				p.State = "blocked"
			}
			r.Paths = append(r.Paths, p)
		}
	}
	return r
}
