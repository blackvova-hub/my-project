package macroapi

// Only a small, attributed subset of public-domain government series is used.
// Proprietary S&P, Nasdaq, ICE DXY and LBMA series are deliberately excluded.
func catalog() []Series {
	defs := []struct{ id, title, unit, freq, category, provider, description string }{
		{"CPIAUCSL", "CPI · США", "индекс 1982–84 = 100", "monthly", "inflation", "BLS", "Индекс потребительских цен, все товары; сезонно скорректирован. Это уровень цен, а не годовая инфляция."},
		{"CPILFESL", "Core CPI · США", "индекс 1982–84 = 100", "monthly", "inflation", "BLS", "Потребительские цены без продуктов питания и энергии; сезонно скорректированный индекс."},
		{"PCEPI", "PCE · США", "индекс 2017 = 100", "monthly", "inflation", "BEA", "Индекс цен расходов на личное потребление. Значения индекса нельзя читать как проценты инфляции."},
		{"PCEPILFE", "Core PCE · США", "индекс 2017 = 100", "monthly", "inflation", "BEA", "Индекс цен личного потребления без продуктов питания и энергии; одна из метрик инфляции ФРС."},
		{"FEDFUNDS", "Эффективная ставка ФРС", "%", "monthly", "rates", "Federal Reserve", "Средняя за месяц эффективная ставка federal funds; отличается от целевого диапазона FOMC."},
		{"DFEDTARU", "ФРС · верхняя граница", "%", "daily", "rates", "Federal Reserve", "Верхняя граница целевого диапазона ставки federal funds."},
		{"UNRATE", "Безработица · США", "%", "monthly", "labor", "BLS", "Доля безработных в рабочей силе, сезонно скорректированная."},
		{"PAYEMS", "Рабочие места · США", "тыс. человек", "monthly", "labor", "BLS", "Общее число занятых вне сельского хозяйства; уровень занятости, а не месячное изменение NFP."},
		{"GDPC1", "Реальный ВВП · США", "млрд USD 2017", "quarterly", "growth", "BEA", "Реальный ВВП в цепных долларах 2017 года, сезонно скорректированный годовой уровень."},
		{"A191RL1Q225SBEA", "Рост реального ВВП · США", "% годовых", "quarterly", "growth", "BEA", "Изменение реального ВВП к предыдущему кварталу, приведённое к годовому темпу."},
		{"INDPRO", "Промышленное производство", "индекс 2017 = 100", "monthly", "growth", "Federal Reserve", "Индекс выпуска промышленности США, сезонно скорректированный."},
		{"M2SL", "Денежная масса M2", "млрд USD", "monthly", "liquidity", "Federal Reserve", "Наличные, ликвидные депозиты и другие компоненты денежного агрегата M2; сезонно скорректирован."},
		{"DGS2", "US Treasury · 2Y", "%", "daily", "yields", "US Treasury", "Доходность казначейских облигаций постоянного срока 2 года; закрытие рабочего дня."},
		{"DGS10", "US Treasury · 10Y", "%", "daily", "yields", "US Treasury", "Доходность казначейских облигаций постоянного срока 10 лет; закрытие рабочего дня."},
		{"DGS30", "US Treasury · 30Y", "%", "daily", "yields", "US Treasury", "Доходность казначейских облигаций постоянного срока 30 лет."},
		{"T10Y2Y", "Кривая · 10Y − 2Y", "п.п.", "daily", "yields", "Federal Reserve Bank of St. Louis", "Спред доходностей 10-летних и 2-летних US Treasury. Отрицательный спред означает инверсию, но не определяет дату рецессии."},
		{"DTWEXBGS", "Доллар · широкий индекс", "индекс январь 2006 = 100", "daily", "currencies", "Federal Reserve", "Номинальный торгово-взвешенный широкий индекс доллара. Это не ICE DXY: корзина и методика различаются."},
		{"WALCL", "Баланс ФРС", "млн USD", "weekly", "liquidity", "Federal Reserve", "Совокупные активы Федеральной резервной системы; наблюдение на среду."},
		{"RRPONTSYD", "Обратное РЕПО ФРС", "млрд USD", "daily", "liquidity", "Federal Reserve Bank of New York", "Объём операций overnight reverse repo; показатель размещённой ликвидности, а не самостоятельный торговый сигнал."},
		{"WTREGEN", "Счёт Казначейства (TGA)", "млн USD", "weekly", "liquidity", "Federal Reserve", "Остаток Treasury General Account в ФРС, недельное среднее."},
	}
	result := make([]Series, 0, len(defs)+2)
	for _, d := range defs {
		s := Series{ID: d.id, Title: d.title, Unit: d.unit, Frequency: d.freq, Category: d.category, Country: "US", Currency: "USD", Source: d.provider + " via FRED", SourceURL: "https://fred.stlouisfed.org/series/" + d.id, Description: d.description, Points: []Point{}, Status: "unavailable", RevisionNote: "Данные могут публиковаться с задержкой и пересматриваться. Дата точки — период наблюдения, а не момент публикации. Показаны последние доступные пересмотренные значения.", Explanation: explain(d.category), Scenarios: scenarios(d.category)}
		result = append(result, s)
	}
	result = append(result,
		Series{ID: "ECB_DFR", Title: "Депозитная ставка ECB", Description: "Официальная ставка депозитной facility ЕЦБ, даты изменения ставки.", Country: "EA", Currency: "EUR", Category: "rates", Unit: "%", Frequency: "on change", Source: "ECB", SourceURL: "https://data.ecb.europa.eu/data/datasets/FM/FM.B.U2.EUR.4F.KR.DFR.LEV", Points: []Point{}, Status: "unavailable", RevisionNote: "Наблюдения публикуются только на даты изменения. Текущая ставка может сохраняться много месяцев.", Explanation: explain("rates"), Scenarios: scenarios("rates")},
		Series{ID: "ECB_EURUSD", Title: "EUR / USD · ECB reference", Description: "Официальный справочный курс ECB: USD за 1 EUR. Дневной fixing, не торгуемая котировка в реальном времени.", Country: "EA", Currency: "EUR", Category: "currencies", Unit: "USD за EUR", Frequency: "daily", Source: "ECB", SourceURL: "https://data.ecb.europa.eu/data/datasets/EXR/EXR.D.USD.EUR.SP00.A", Points: []Point{}, Status: "unavailable", RevisionNote: "Справочный курс обновляется в рабочие дни; выходные и праздники не заполняются вымышленными точками.", Explanation: explain("currencies"), Scenarios: scenarios("currencies")},
	)
	return result
}

func explain(category string) string {
	switch category {
	case "inflation":
		return "Инфляция показывает рост цен. Для рынка важно, отличается ли публикация от ожиданий, и что это меняет в будущей политике центрального банка. Уровень ценового индекса и темп инфляции — разные показатели."
	case "rates":
		return "Ставка определяет стоимость краткосрочных денег. Более высокая ставка обычно повышает стоимость заимствований и дисконтирования будущих доходов. Реакция рынка зависит от того, что уже учтено в цене."
	case "labor":
		return "Сильный рынок труда может поддерживать потребление и удерживать инфляционное давление. Слабость занятости иногда повышает ожидания снижения ставок, одновременно усиливая риски замедления."
	case "growth":
		return "Производство и ВВП описывают активность экономики. Сильный рост может поддерживать прибыль компаний, но также усиливать ожидания более жёсткой денежной политики."
	case "liquidity":
		return "Ликвидность — доступность денег в финансовой системе. Баланс ФРС, M2, TGA и reverse repo отражают разные механизмы и имеют разные даты и единицы; их нельзя бездумно складывать."
	case "yields":
		return "Доходность облигаций — стоимость государственного заимствования и ориентир для других активов. Спред длинных и коротких сроков показывает форму кривой, но не гарантирует конкретный исход."
	default:
		return "Курс валюты отражает относительную стоимость денег. Изменения могут зависеть от ставок, торговли и спроса на защитные активы. Широкий индекс доллара не равен DXY."
	}
}

func scenarios(category string) []string {
	if category == "inflation" || category == "rates" || category == "yields" {
		return []string{"Сценарий: более жёсткие ожидания по ставкам могут давить на BTC, ETH и акции через рост доходностей; доллар может укрепиться.", "Сценарий: снижение инфляционного давления может поддержать рисковые активы, если оно не сопровождается резким ухудшением роста.", "Это условные сценарии, не прогноз: результат зависит от ожиданий, позиционирования и сопутствующих новостей."}
	}
	return []string{"Сценарий: рост активности и доступной ликвидности может поддержать рисковые активы при стабильной инфляции.", "Сценарий: ухудшение роста может усилить спрос на защиту; ожидания снижения ставок не гарантируют роста BTC или акций.", "Это условные сценарии, не прогноз; историческая корреляция не доказывает причинность."}
}
