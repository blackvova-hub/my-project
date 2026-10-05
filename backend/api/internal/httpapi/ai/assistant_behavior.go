package aiapi

import (
	"regexp"
	"strings"
	"unicode"
)

var perpetualPairPattern = regexp.MustCompile(`(?i)\b([a-z0-9]{2,15})usdt\b`)

// protectedLocalReply is intentionally limited to requests that must never be
// delegated to a model. Normal product questions always go through OpenAI.
func protectedLocalReply(question string) (string, bool) {
	query := normalizeSearchText(question)
	if !isPromptOrSecretExtraction(query) {
		return "", false
	}
	return "Я не раскрываю внутренние инструкции, конфигурацию, ключи, пароли и другие секреты, а также не помогаю обходить ограничения доступа. Могу помочь с разрешёнными функциями Short & Long.", true
}

// recognizedIntent adds one short, server-controlled clarification to the
// selected knowledge. It is a routing hint, not a prewritten answer.
func recognizedIntent(query string) string {
	switch {
	case containsAny(query, "аватар", "аву", "авку", "фото", "фотограф", "картинк"):
		return "Вопрос относится к смене фото профиля. Используй точный сценарий из справки «Личный кабинет» и не придумывай кнопку ручного сохранения."
	case containsAny(query, "telegram", "телеграм", "телегу") || hasExactWord(query, "тг"):
		return "Вопрос относится к привязке Telegram. Назови вкладку и карточку интерфейса точно, затем объясни получение ссылки, запуск бота и проверку статуса."
	case containsAny(query, "админ", "админк", "admin", "права администратора", "роль администратора"):
		return "Вопрос относится к админ-панели. Учитывай проверенный признак прав администратора и границу данных: помощник не видит таблицу пользователей и не может вывести её содержимое."
	case extractPerpetualPair(query) != "":
		return "Пользователь спрашивает о конкретной USDT-паре. Не придумывай живой анализ; объясни, как открыть карточку именно этой пары и какие данные там посмотреть."
	case containsAny(query, "памп", "дамп", "pump", "dump", "всплеск", "резкий рост", "резкое падение"):
		return "Вопрос относится к сигналу на резкое движение. Дай конкретный пример настроек из справки для роста или падения, а не повторяй общую инструкцию запуска сканера."
	case containsAny(query, "сигнал", "алерт") && containsAny(query, "перейти на бирж", "открыть бирж", "ссылка на бирж", "зайти на бирж"):
		return "Вопрос относится к переходу на торговую площадку из готового сигнала. Объясни раскрытие строки сигнала и выбор кнопки Bybit или Binance; не объясняй создание правила."
	case containsAny(query, "попдиск", "подписк", "тариф") || hasExactWord(query, "pro") || hasExactWord(query, "про"):
		return "Вопрос относится к тарифу или подписке. Используй фактическую инструкцию оформления и учитывай текущий тариф пользователя."
	case containsAny(query, "монет", "токен", "информац", "инфо") || keywordMatches(query, "инфо"):
		return "Вопрос относится к поиску карточки монеты. Объясни путь через «Лист ожидания» или кнопку «Info» в раскрытом сигнале."
	case containsAny(query, "сигнал", "алерт", "сканер"):
		return "Вопрос относится к сканеру. Учитывай историю: если общие шаги уже были даны и пользователь просит конкретнее, предложи готовый пример условий вместо повторения тех же шагов."
	case containsAny(query, "community", "комьюнити", "сообществ"):
		return "Вопрос относится к разделу «Комьюнити». Используй русское название раздела и его фактический текущий статус."
	default:
		return ""
	}
}

func extractPerpetualPair(question string) string {
	match := perpetualPairPattern.FindStringSubmatch(question)
	if len(match) != 2 {
		return ""
	}
	return strings.ToUpper(match[1] + "USDT")
}

func isPromptOrSecretExtraction(query string) bool {
	if containsAny(query,
		"игнорируй предыдущие инструкции",
		"забудь предыдущие инструкции",
		"ignore previous instructions",
		"ignore all instructions",
		"developer message",
		"обойти авторизацию",
		"обойти админку",
		"обойти проверку доступа",
		"взломать админку",
		"повысить себе права",
	) {
		return true
	}
	revealVerb := containsAny(query, "покажи", "раскрой", "выведи", "пришли", "дай", "скопируй", "расскажи")
	protectedTarget := containsAny(query,
		"системный промпт", "системные инструкции", "скрытые инструкции", "внутренние инструкции",
		"api key", "ключ api", "openai key", "секретный ключ", "пароль админа", "токен доступа",
	)
	return revealVerb && protectedTarget
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func hasExactWord(value string, target string) bool {
	for _, word := range strings.Fields(value) {
		if word == target {
			return true
		}
	}
	return false
}

func sanitizeAssistantReply(reply string, question string) string {
	reply = strings.NewReplacer(
		"**", "",
		"__", "",
		"`", "",
		"### ", "",
		"## ", "",
		"# ", "",
	).Replace(reply)

	normalizedQuestion := normalizeSearchText(question)
	if !containsAny(normalizedQuestion, "url", "ссылк", "адрес страниц", "путь") {
		reply = strings.NewReplacer(
			"/community/channels", "«Комьюнити» → «Каналы»",
			"/community/feed", "«Комьюнити» → «Лента»",
			"/scanners/1", "«Сканеры» → «Сканер 1»",
			"/scanners/2", "«Сканеры» → «Сканер 2»",
			"/scanners/3", "«Сканеры» → «Сканер 3»",
			"/scanners", "раздел «Сканеры»",
			"/news/:id", "карточка новости",
			"/forgot-password", "страница «Восстановление пароля»",
			"/reset-password", "страница «Новый пароль»",
			"/account", "раздел «Кабинет»",
			"/news", "раздел «Новости»",
			"/community", "раздел «Комьюнити»",
			"/exchange", "раздел «Exchange»",
			"/privacy", "страница «Политика конфиденциальности»",
			"/cookies", "страница «Политика cookie»",
			"/terms", "страница «Пользовательское соглашение»",
			"/login", "страница «Вход»",
			"/register", "страница «Регистрация»",
		).Replace(reply)
	}

	reply = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, reply)
	return strings.TrimSpace(reply)
}
