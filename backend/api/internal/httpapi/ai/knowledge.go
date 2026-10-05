package aiapi

import (
	"embed"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxSelectedKnowledgeDocuments = 3
	maxConversationMessages       = 6
	maxConversationMessageRunes   = 1600
	maxAIOutputTokens             = 900
)

// knowledgeFiles contains the whole site handbook, but only the documents
// selected for the current page and question are sent to OpenAI.
//
//go:embed knowledge/*.md
var knowledgeFiles embed.FS

type knowledgeDocument struct {
	ID       string
	Title    string
	File     string
	Routes   []string
	Keywords []string
}

type selectedKnowledge struct {
	PagePath     string
	Documents    []knowledgeDocument
	Instructions string
}

var knowledgeCatalog = []knowledgeDocument{
	{
		ID:     "home",
		Title:  "Главная и терминал",
		File:   "home.md",
		Routes: []string{"/"},
		Keywords: []string{
			"главн", "терминал", "дашборд", "избранн", "watchlist", "обзор рынка", "исследован",
		},
	},
	{
		ID:     "account",
		Title:  "Личный кабинет",
		File:   "account.md",
		Routes: []string{"/account"},
		Keywords: []string{
			"аккаунт", "кабинет", "профил", "аватар", "аву", "авку", "фото", "фотограф", "картинк", "имя", "страна", "основная биржа", "сессии", "устройств",
		},
	},
	{
		ID:     "scanners",
		Title:  "Сканеры и сигналы",
		File:   "scanners.md",
		Routes: []string{"/scanners"},
		Keywords: []string{
			"сканер", "сигнал", "услови", "ликвидац", "дельт", "cvd", "открытый интерес", "open interest", "порог", "памп", "дамп", "pump", "dump", "всплеск", "перейти на бирж", "открыть бирж", "and", "not",
		},
	},
	{
		ID:    "coin_research",
		Title: "Информация о монете",
		File:  "coin_research.md",
		Keywords: []string{
			"монет", "токен", "инфо", "информац", "что скажешь о", "расскажи о", "исследован", "coingecko", "dexscreener", "funding", "ликвидност",
		},
	},
	{
		ID:    "charts",
		Title: "Графики и индикаторы",
		File:  "charts.md",
		Keywords: []string{
			"график", "свеч", "таймфрейм", "sma", "ema", "rsi", "macd", "bollinger", "vwap", "supertrend", "ichimoku", "smart money", "order block", "fvg", "bos", "choch",
		},
	},
	{
		ID:    "trades",
		Title: "Сделки и статистика",
		File:  "trades.md",
		Keywords: []string{
			"сделк", "торговая статистика", "стратег", "прибыл", "убыт", "винрейт", "win rate", "закрыть пози", "скриншот", "точка входа",
		},
	},
	{
		ID:     "news",
		Title:  "Новости",
		File:   "news.md",
		Routes: []string{"/news"},
		Keywords: []string{
			"новост", "календар", "важные события", "сводк", "событие биржи", "сегодня в фокусе",
		},
	},
	{
		ID:    "subscriptions",
		Title: "Тарифы и доступ",
		File:  "subscriptions.md",
		Keywords: []string{
			"тариф", "подписк", "попдиск", "standard", "стандарт", "pro", "free", "цена", "стоимость", "оплат", "купить", "оформить", "получить", "доступ", "1500", "3000",
		},
	},
	{
		ID:    "telegram",
		Title: "Telegram-уведомления",
		File:  "telegram.md",
		Keywords: []string{
			"telegram", "телеграм", "тг", "бот", "уведомлен", "привяз", "отвяз", "код привязки",
		},
	},
	{
		ID:    "security",
		Title: "Безопасность и 2FA",
		File:  "security.md",
		Keywords: []string{
			"2fa", "двухфактор", "безопасност", "подтвердить email", "подтверждение email", "сеанс", "сессия", "парол",
		},
	},
	{
		ID:     "auth",
		Title:  "Вход и восстановление доступа",
		File:   "auth.md",
		Routes: []string{"/login", "/register", "/forgot-password", "/reset-password"},
		Keywords: []string{
			"войти", "вход в аккаунт", "регистрац", "забыли пароль", "забыл пароль", "сброс пароля", "восстанов", "код на email",
		},
	},
	{
		ID:     "admin_access",
		Title:  "Доступ к админ-панели",
		File:   "admin_access.md",
		Routes: []string{"/admin"},
		Keywords: []string{
			"админ", "админк", "admin", "права администратора", "роль администратора", "панель администратора",
		},
	},
	{
		ID:     "availability",
		Title:  "Community и Exchange",
		File:   "availability.md",
		Routes: []string{"/community", "/exchange"},
		Keywords: []string{
			"community", "комьюнити", "сообществ", "канал", "лента сообщества", "exchange", "биржа стратег", "маркетплейс",
		},
	},
	{
		ID:     "legal",
		Title:  "Правовые документы и cookie",
		File:   "legal.md",
		Routes: []string{"/terms", "/privacy", "/cookies"},
		Keywords: []string{
			"правил", "условия", "соглашен", "конфиденциаль", "персональн", "cookie", "куки", "возврат", "претензи", "support@",
		},
	},
	{
		ID:    "navigation",
		Title: "Навигация по сайту",
		File:  "navigation.md",
		Keywords: []string{
			"где найти", "куда нажать", "как перейти", "какая страница", "раздел сайта", "навигац", "ссылка на",
		},
	},
}

func selectKnowledge(contextValue string, messages []ChatMessage, userContext assistantUserContext) selectedKnowledge {
	pagePath := extractPagePath(contextValue)
	query := selectionQuery(messages)
	type scoredDocument struct {
		document   knowledgeDocument
		routeMatch bool
		score      int
		order      int
	}

	scored := make([]scoredDocument, 0, len(knowledgeCatalog))
	hasTopicMatch := false
	for order, document := range knowledgeCatalog {
		routeMatch := documentMatchesRoute(document, pagePath)
		score := 0
		for _, keyword := range document.Keywords {
			if keywordMatches(query, normalizeSearchText(keyword)) {
				score++
			}
		}
		if document.ID == "coin_research" && extractPerpetualPair(query) != "" {
			score += 2
		}
		if routeMatch || score > 0 {
			if score > 0 && document.ID != "navigation" {
				hasTopicMatch = true
			}
			scored = append(scored, scoredDocument{
				document: document, routeMatch: routeMatch, score: score, order: order,
			})
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].routeMatch != scored[j].routeMatch {
			return scored[i].routeMatch
		}
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].order < scored[j].order
	})

	documents := make([]knowledgeDocument, 0, maxSelectedKnowledgeDocuments)
	seen := make(map[string]struct{}, maxSelectedKnowledgeDocuments)
	for _, candidate := range scored {
		if candidate.document.ID == "navigation" && hasTopicMatch {
			continue
		}
		if _, ok := seen[candidate.document.ID]; ok {
			continue
		}
		seen[candidate.document.ID] = struct{}{}
		documents = append(documents, candidate.document)
		if len(documents) == maxSelectedKnowledgeDocuments {
			break
		}
	}
	if len(documents) == 0 {
		documents = append(documents, documentByID("navigation"))
	}

	return selectedKnowledge{
		PagePath:     pagePath,
		Documents:    documents,
		Instructions: buildInstructions(pagePath, documents, userContext, recognizedIntent(query)),
	}
}

func selectionQuery(messages []ChatMessage) string {
	latestIndex := -1
	latest := ""
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			latestIndex = index
			latest = normalizeSearchText(messages[index].Content)
			break
		}
	}
	if latestIndex < 0 || latest == "" || !needsPreviousTopic(latest) {
		return latest
	}
	for index := latestIndex - 1; index >= 0; index-- {
		if !strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			continue
		}
		previous := normalizeSearchText(messages[index].Content)
		if previous != "" {
			return previous + " " + latest
		}
	}
	return latest
}

func needsPreviousTopic(latest string) bool {
	explicitFollowUp := containsAny(latest, "конкретн", "что дальше", "чо дальше", "и что", "и чо", "ну а", "поясни", "подробнее")
	if explicitFollowUp {
		return true
	}
	return len(strings.Fields(latest)) <= 4 && recognizedIntent(latest) == ""
}

type assistantUserContext struct {
	Plan            string
	PrimaryExchange string
	EmailVerified   bool
	TwoFAEnabled    bool
	IsAdmin         bool
}

func buildInstructions(pagePath string, documents []knowledgeDocument, userContext assistantUserContext, intent string) string {
	var b strings.Builder
	b.WriteString(readKnowledgeFile("core.md"))
	for _, document := range documents {
		b.WriteString("\n\n## Справка: ")
		b.WriteString(document.Title)
		b.WriteString("\n")
		b.WriteString(readKnowledgeFile(document.File))
	}

	if pagePath == "" {
		pagePath = "не определена"
	}
	plan := strings.ToLower(strings.TrimSpace(userContext.Plan))
	if plan == "" {
		plan = "не определён"
	}
	exchange := strings.ToLower(strings.TrimSpace(userContext.PrimaryExchange))
	if exchange == "" {
		exchange = "не выбрана"
	}
	b.WriteString("\n\n## Проверенный контекст текущего пользователя\n")
	fmt.Fprintf(&b, "Текущая страница: %s. Тариф: %s. Основная биржа: %s. Email подтверждён: %s. 2FA включена: %s. Права администратора: %s.",
		pagePath, plan, exchange, yesNo(userContext.EmailVerified), yesNo(userContext.TwoFAEnabled), yesNo(userContext.IsAdmin))
	if intent != "" {
		b.WriteString("\n\n## Уточнение темы текущего вопроса\n")
		b.WriteString(intent)
	}
	return strings.TrimSpace(b.String())
}

func buildOpenAIInput(messages []ChatMessage) []openAIInputMessage {
	filtered := make([]openAIInputMessage, 0, len(messages))
	skipAssistantAfterBlockedUser := false
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := truncateRunes(strings.TrimSpace(message.Content), maxConversationMessageRunes)
		if content == "" {
			continue
		}
		if role == "user" {
			if isPromptOrSecretExtraction(normalizeSearchText(content)) {
				skipAssistantAfterBlockedUser = true
				continue
			}
			skipAssistantAfterBlockedUser = false
		} else if skipAssistantAfterBlockedUser {
			skipAssistantAfterBlockedUser = false
			continue
		}
		filtered = append(filtered, openAIInputMessage{Role: role, Content: content})
	}
	if len(filtered) > maxConversationMessages {
		filtered = filtered[len(filtered)-maxConversationMessages:]
	}
	return filtered
}

func estimateRequestTokens(instructions string, messages []openAIInputMessage) int {
	total := estimateTokens(instructions)
	for _, message := range messages {
		total += estimateTokens(message.Role) + estimateTokens(message.Content) + 4
	}
	return total
}

func latestUserMessage(messages []ChatMessage) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if strings.EqualFold(strings.TrimSpace(messages[index].Role), "user") {
			return strings.TrimSpace(messages[index].Content)
		}
	}
	return ""
}

func extractPagePath(contextValue string) string {
	for _, line := range strings.Split(contextValue, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < len("Page:") || !strings.EqualFold(line[:len("Page:")], "Page:") {
			continue
		}
		candidate := strings.TrimSpace(line[len("Page:"):])
		if queryIndex := strings.IndexByte(candidate, '?'); queryIndex >= 0 {
			candidate = candidate[:queryIndex]
		}
		if hashIndex := strings.IndexByte(candidate, '#'); hashIndex >= 0 {
			candidate = candidate[:hashIndex]
		}
		if isSafePagePath(candidate) {
			return candidate
		}
	}
	return ""
}

func isSafePagePath(value string) bool {
	if value == "" || value[0] != '/' || len(value) > 160 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("/-_", r) {
			continue
		}
		return false
	}
	return true
}

func documentMatchesRoute(document knowledgeDocument, pagePath string) bool {
	for _, route := range document.Routes {
		if route == "/" {
			if pagePath == route {
				return true
			}
			continue
		}
		if pagePath == route || strings.HasPrefix(pagePath, route+"/") {
			return true
		}
	}
	return false
}

func documentByID(id string) knowledgeDocument {
	for _, document := range knowledgeCatalog {
		if document.ID == id {
			return document
		}
	}
	panic("unknown AI knowledge document: " + id)
}

func readKnowledgeFile(name string) string {
	content, err := knowledgeFiles.ReadFile("knowledge/" + name)
	if err != nil {
		panic("read AI knowledge file " + name + ": " + err.Error())
	}
	return strings.TrimSpace(string(content))
}

func normalizeSearchText(value string) string {
	value = strings.ToLower(value)
	value = strings.Map(func(r rune) rune {
		if r == 'ё' {
			return 'е'
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return ' '
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func keywordMatches(query string, keyword string) bool {
	if query == "" || keyword == "" {
		return false
	}
	if isShortASCIIKeyword(keyword) {
		for _, word := range strings.Fields(query) {
			if word == keyword {
				return true
			}
		}
		return false
	}
	if strings.Contains(query, keyword) {
		return true
	}
	if strings.ContainsRune(keyword, ' ') {
		return false
	}
	for _, word := range strings.Fields(query) {
		if fuzzyWordMatch(word, keyword) {
			return true
		}
	}
	return false
}

func fuzzyWordMatch(word string, keyword string) bool {
	wordRunes := []rune(word)
	keywordRunes := []rune(keyword)
	if len(wordRunes) < 4 || len(keywordRunes) < 4 {
		return false
	}
	difference := len(wordRunes) - len(keywordRunes)
	if difference < 0 {
		difference = -difference
	}
	if difference > 2 {
		return false
	}
	return damerauLevenshteinWithin(wordRunes, keywordRunes, 1)
}

func damerauLevenshteinWithin(left []rune, right []rune, limit int) bool {
	rows := len(left) + 1
	columns := len(right) + 1
	distance := make([][]int, rows)
	for row := range distance {
		distance[row] = make([]int, columns)
		distance[row][0] = row
	}
	for column := 0; column < columns; column++ {
		distance[0][column] = column
	}
	for row := 1; row < rows; row++ {
		rowMinimum := limit + 1
		for column := 1; column < columns; column++ {
			cost := 1
			if left[row-1] == right[column-1] {
				cost = 0
			}
			value := min(
				distance[row-1][column]+1,
				distance[row][column-1]+1,
				distance[row-1][column-1]+cost,
			)
			if row > 1 && column > 1 && left[row-1] == right[column-2] && left[row-2] == right[column-1] {
				value = min(value, distance[row-2][column-2]+1)
			}
			distance[row][column] = value
			if value < rowMinimum {
				rowMinimum = value
			}
		}
		if rowMinimum > limit && row > limit && row < rows-1 {
			continue
		}
	}
	return distance[rows-1][columns-1] <= limit
}

func isShortASCIIKeyword(keyword string) bool {
	if len(keyword) == 0 || len(keyword) > 3 {
		return false
	}
	for _, r := range keyword {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit]))
}

func yesNo(value bool) string {
	if value {
		return "да"
	}
	return "нет"
}
