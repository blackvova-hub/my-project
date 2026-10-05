package telegramapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"backend/internal/auth"
)

type Config struct {
	BotToken      string
	WebhookSecret string
	BotUsername   string
}

type API struct {
	store   *auth.Store
	cfg     Config
	baseURL string
}

func New(store *auth.Store, cfg Config) *API {
	return &API{store: store, cfg: cfg, baseURL: "https://api.telegram.org"}
}

type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type getUpdatesResponse struct {
	OK     bool     `json:"ok"`
	Result []Update `json:"result"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	From      *User  `json:"from"`
	Text      string `json:"text"`
}

type CallbackQuery struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Message Message `json:"message"`
	Data    string  `json:"data"`
}

type Chat struct {
	ID int64 `json:"id"`
}

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

type sendMessageReq struct {
	ChatID      int64  `json:"chat_id"`
	Text        string `json:"text"`
	ParseMode   string `json:"parse_mode,omitempty"`
	ReplyMarkup any    `json:"reply_markup,omitempty"`
	DisableWeb  bool   `json:"disable_web_page_preview,omitempty"`
}

type answerCallbackReq struct {
	CallbackID string `json:"callback_query_id"`
	Text       string `json:"text,omitempty"`
	ShowAlert  bool   `json:"show_alert,omitempty"`
}

func (a *API) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(a.cfg.BotToken) == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if a.cfg.WebhookSecret != "" {
		secret := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
		if secret != a.cfg.WebhookSecret {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var upd Update
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&upd); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	a.handleUpdate(r.Context(), &upd)

	w.WriteHeader(http.StatusOK)
}

// RunPolling is a local-development transport for machines that cannot expose
// the HTTPS webhook. Production must keep TELEGRAM_POLLING_ENABLED disabled.
func (a *API) RunPolling(ctx context.Context) error {
	if strings.TrimSpace(a.cfg.BotToken) == "" {
		return errors.New("bot_token_missing")
	}

	var offset int64
	retryDelay := time.Second
	for {
		updates, err := a.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			log.Printf("telegram polling (LOCAL_ONLY): getUpdates failed: %v", err)
			timer := time.NewTimer(retryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			retryDelay *= 2
			if retryDelay > 15*time.Second {
				retryDelay = 15 * time.Second
			}
			continue
		}

		retryDelay = time.Second
		for index := range updates {
			update := &updates[index]
			a.handleUpdate(ctx, update)
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
		}
	}
}

func (a *API) getUpdates(ctx context.Context, offset int64) ([]Update, error) {
	query := url.Values{}
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("timeout", "25")
	query.Set("limit", "100")
	query.Set("allowed_updates", `["message","callback_query"]`)
	endpoint := a.methodURL("getUpdates") + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("telegram_polling_request_failed")
	}
	client := &http.Client{Timeout: 35 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("telegram_polling_transport_failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return nil, errors.New("telegram_polling_api_failed")
	}

	var payload getUpdatesResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, errors.New("telegram_polling_response_failed")
	}
	if !payload.OK {
		return nil, errors.New("telegram_polling_api_failed")
	}
	return payload.Result, nil
}

func (a *API) handleUpdate(ctx context.Context, upd *Update) {
	request := (&http.Request{}).WithContext(ctx)
	switch {
	case upd.Message != nil:
		a.handleMessage(request, upd.Message)
	case upd.CallbackQuery != nil:
		a.handleCallback(request, upd.CallbackQuery)
	}
}

func (a *API) handleMessage(r *http.Request, msg *Message) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	lower := strings.ToLower(text)
	if isProfileCommand(lower) {
		a.sendProfileMessage(r.Context(), msg.Chat.ID)
		return
	}
	if lower == "/cancel" || lower == "отмена" {
		_ = a.store.ClearTelegramLimitRequest(r.Context(), msg.Chat.ID)
		_ = a.sendMessage(msg.Chat.ID, "Действие отменено.", nil)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if a.handleLimitFlow(r.Context(), msg.Chat.ID, text) {
		return
	}
	if strings.HasPrefix(lower, "/start") {
		parts := strings.SplitN(text, " ", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
			a.handleStartLink(r, msg, strings.TrimSpace(parts[1]))
			return
		}
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if isSettingsCommand(lower) {
		a.sendSettingsMenu(msg.Chat.ID)
		return
	}
	if lower == "лимит сигналов по паре" {
		_ = a.store.UpsertTelegramLimitRequest(r.Context(), msg.Chat.ID, "limit", "*")
		_ = a.sendMessage(msg.Chat.ID, "Отправь число лимита на 24 часа для каждой пары. 0 = отключить лимит.", nil)
		return
	}
	if lower == "закрыть" {
		_ = a.store.ClearTelegramLimitRequest(r.Context(), msg.Chat.ID)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if lower == "/menu" || lower == "меню" {
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if isStatusCommand(lower) {
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if isTelegramToggleOn(lower) {
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), msg.Chat.ID, true)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if isTelegramToggleOff(lower) {
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), msg.Chat.ID, false)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if isScannerToggleOn(lower, "1") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", true)
		return
	}
	if isScannerToggleOff(lower, "1") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", false)
		return
	}
	if isScannerToggleOn(lower, "2") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", true)
		return
	}
	if isScannerToggleOff(lower, "2") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", false)
		return
	}
	if isScannerToggleOn(lower, "3") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", true)
		return
	}
	if isScannerToggleOff(lower, "3") {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", false)
		return
	}
	if lower == "/scanner1_on" || lower == "/scan1_on" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", true)
		return
	}
	if lower == "/scanner1_off" || lower == "/scan1_off" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", false)
		return
	}
	if lower == "/scanner2_on" || lower == "/scan2_on" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", true)
		return
	}
	if lower == "/scanner2_off" || lower == "/scan2_off" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", false)
		return
	}
	if lower == "/scanner3_on" || lower == "/scan3_on" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", true)
		return
	}
	if lower == "/scanner3_off" || lower == "/scan3_off" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", false)
		return
	}
	if lower == "/scanners_on" || lower == "/scan_on" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", true)
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", true)
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", true)
		return
	}
	if lower == "/scanners_off" || lower == "/scan_off" {
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_1", false)
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_2", false)
		a.toggleScanner(r.Context(), msg.Chat.ID, "SLOT_3", false)
		return
	}
	if lower == "/on" || lower == "on" {
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), msg.Chat.ID, true)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
	if lower == "/off" || lower == "/stop" || lower == "off" {
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), msg.Chat.ID, false)
		a.sendStatusMessage(r.Context(), msg.Chat.ID)
		return
	}
}

func (a *API) handleStartLink(r *http.Request, msg *Message, token string) {
	userID, err := a.store.ConsumeTelegramLinkToken(r.Context(), token)
	if err != nil {
		_ = a.sendMessage(msg.Chat.ID, "Код недействителен или истёк. Попробуй снова.", nil)
		return
	}
	username := ""
	if msg.From != nil {
		username = msg.From.Username
	}
	if err := a.store.SetTelegramLink(r.Context(), userID, msg.Chat.ID, username); err != nil {
		_ = a.sendMessage(msg.Chat.ID, "Не удалось привязать Telegram. Попробуй ещё раз.", nil)
		return
	}
	_ = a.sendMessage(msg.Chat.ID, "Telegram успешно привязан. Управляй уведомлениями и сканерами кнопками ниже.", nil)
	a.sendStatusMessage(r.Context(), msg.Chat.ID)
}

func (a *API) handleCallback(r *http.Request, cb *CallbackQuery) {
	switch cb.Data {
	case "tg_on":
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), cb.From.ID, true)
		_ = a.answerCallback(cb.ID, "Уведомления включены")
		a.sendStatusMessage(r.Context(), cb.Message.Chat.ID)
	case "tg_off":
		_ = a.store.UpdateTelegramEnabledByTelegramID(r.Context(), cb.From.ID, false)
		_ = a.answerCallback(cb.ID, "Уведомления выключены")
		a.sendStatusMessage(r.Context(), cb.Message.Chat.ID)
	case "scan1_on":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_1", true)
		_ = a.answerCallback(cb.ID, "Сканер 1 включен")
	case "scan1_off":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_1", false)
		_ = a.answerCallback(cb.ID, "Сканер 1 выключен")
	case "scan2_on":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_2", true)
		_ = a.answerCallback(cb.ID, "Сканер 2 включен")
	case "scan2_off":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_2", false)
		_ = a.answerCallback(cb.ID, "Сканер 2 выключен")
	case "scan3_on":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_3", true)
		_ = a.answerCallback(cb.ID, "Сканер 3 включен")
	case "scan3_off":
		a.toggleScanner(r.Context(), cb.From.ID, "SLOT_3", false)
		_ = a.answerCallback(cb.ID, "Сканер 3 выключен")
	case "settings":
		_ = a.answerCallback(cb.ID, "Настройки")
		a.sendSettingsMenu(cb.Message.Chat.ID)
	case "limit_symbol":
		_ = a.store.UpsertTelegramLimitRequest(r.Context(), cb.Message.Chat.ID, "limit", "*")
		_ = a.answerCallback(cb.ID, "Лимит по паре")
		_ = a.sendMessage(cb.Message.Chat.ID, "Отправь число лимита на 24 часа для каждой пары. 0 = отключить лимит.", nil)
	case "settings_close":
		_ = a.answerCallback(cb.ID, "Закрыто")
	default:
		_ = a.answerCallback(cb.ID, "Неизвестная команда")
	}
}

func (a *API) sendStatusMessage(ctx context.Context, chatID int64) {
	enabled, err := a.store.GetTelegramEnabledByTelegramID(ctx, chatID)
	if err != nil {
		enabled = false
	}
	scan1, _ := a.store.GetScannerSlotEnabledByTelegramID(ctx, chatID, "SLOT_1")
	scan2, _ := a.store.GetScannerSlotEnabledByTelegramID(ctx, chatID, "SLOT_2")
	scan3, _ := a.store.GetScannerSlotEnabledByTelegramID(ctx, chatID, "SLOT_3")
	status := "Управление уведомлениями и сканерами:\n"
	status += "Telegram: "
	if enabled {
		status += "включены\n"
	} else {
		status += "выключены\n"
	}
	status += "Сканер 1: "
	if scan1 {
		status += "включен\n"
	} else {
		status += "выключен\n"
	}
	status += "Сканер 2: "
	if scan2 {
		status += "включен"
	} else {
		status += "выключен"
	}
	status += "\nСканер 3: "
	if scan3 {
		status += "включен"
	} else {
		status += "выключен"
	}
	_ = a.sendMessage(chatID, status, menuKeyboard(enabled, scan1, scan2, scan3))
}

func menuKeyboard(tgEnabled, scan1Enabled, scan2Enabled, scan3Enabled bool) map[string]any {
	tgLabel := "📣 Уведомления: выкл"
	if tgEnabled {
		tgLabel = "📣 Уведомления: вкл"
	}
	s1Label := "🔧 Сканер 1: выкл"
	if scan1Enabled {
		s1Label = "🔧 Сканер 1: вкл"
	}
	s2Label := "🔧 Сканер 2: выкл"
	if scan2Enabled {
		s2Label = "🔧 Сканер 2: вкл"
	}
	s3Label := "🔧 Сканер 3: выкл"
	if scan3Enabled {
		s3Label = "🔧 Сканер 3: вкл"
	}
	return map[string]any{
		"keyboard": [][]map[string]string{{
			{"text": tgLabel},
		}, {
			{"text": s1Label},
			{"text": s2Label},
			{"text": s3Label},
		}, {
			{"text": "⚙️ Настройки"},
			{"text": "👤 Профиль"},
		}, {
			{"text": "ℹ️ Статус"},
		}},
		"resize_keyboard":   true,
		"one_time_keyboard": false,
	}
}

func isStatusCommand(lower string) bool {
	return lower == "ℹ️ статус" || lower == "status" || lower == "/status"
}

func isSettingsCommand(lower string) bool {
	return lower == "⚙️ настройки" || lower == "настройки" || lower == "/settings"
}

func isProfileCommand(lower string) bool {
	return lower == "👤 профиль" || lower == "профиль" || lower == "/profile"
}

func isTelegramToggleOn(lower string) bool {
	return lower == "📣 уведомления: выкл" || lower == "включить уведомления" || lower == "/tg_on"
}

func isTelegramToggleOff(lower string) bool {
	return lower == "📣 уведомления: вкл" || lower == "выключить уведомления" || lower == "/tg_off"
}

func isScannerToggleOn(lower string, slot string) bool {
	if slot == "1" {
		return lower == "🔧 сканер 1: выкл" || lower == "сканер 1 вкл"
	}
	if slot == "2" {
		return lower == "🔧 сканер 2: выкл" || lower == "сканер 2 вкл"
	}
	return lower == "🔧 сканер 3: выкл" || lower == "сканер 3 вкл"
}

func isScannerToggleOff(lower string, slot string) bool {
	if slot == "1" {
		return lower == "🔧 сканер 1: вкл" || lower == "сканер 1 выкл"
	}
	if slot == "2" {
		return lower == "🔧 сканер 2: вкл" || lower == "сканер 2 выкл"
	}
	return lower == "🔧 сканер 3: вкл" || lower == "сканер 3 выкл"
}

func (a *API) sendSettingsMenu(chatID int64) {
	text := "Настройки Telegram. Здесь можно ограничить число сигналов на каждую пару за 24 часа."
	_ = a.sendMessage(chatID, text, settingsKeyboard())
}

func settingsKeyboard() map[string]any {
	return map[string]any{
		"keyboard": [][]map[string]string{{
			{"text": "Лимит сигналов по паре"},
		}, {
			{"text": "Закрыть"},
		}},
		"resize_keyboard":   true,
		"one_time_keyboard": false,
	}
}

func (a *API) handleLimitFlow(ctx context.Context, chatID int64, text string) bool {
	step, _, ok, err := a.store.GetTelegramLimitRequest(ctx, chatID)
	if err != nil || !ok {
		return false
	}
	if step == "limit" {
		limit, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || limit < 0 || limit > 10000 {
			_ = a.sendMessage(chatID, "Нужно число от 0 до 10000.", nil)
			return true
		}
		userID, err := a.store.GetUserNumIDByTelegramID(ctx, chatID)
		if err != nil {
			_ = a.sendMessage(chatID, "Не удалось определить пользователя. Перепривяжи Telegram.", nil)
			return true
		}
		if err := a.store.UpsertTelegramSignalLimit(ctx, userID, "*", limit); err != nil {
			_ = a.sendMessage(chatID, "Не удалось применить лимит. Попробуй позже.", nil)
			return true
		}
		if err := a.store.ClearTelegramLimitRequest(ctx, chatID); err != nil {
			_ = a.sendMessage(chatID, "Лимит применён, но не удалось завершить режим настройки. Напиши /menu.", nil)
			return true
		}
		if limit == 0 {
			_ = a.sendMessage(chatID, "Лимит сигналов по каждой паре удалён. Успешно применено.", nil)
			return true
		}
		_ = a.sendMessage(chatID, "Лимит сигналов по каждой паре установлен: "+strconv.Itoa(limit)+" за 24 часа. Успешно применено.", nil)
		return true
	}
	return false
}

func (a *API) toggleScanner(ctx context.Context, chatID int64, slot string, enabled bool) {
	if enabled {
		user, err := a.store.GetUserByTelegramID(ctx, chatID)
		if err != nil {
			_ = a.sendMessage(chatID, "Не удалось определить пользователя. Перепривяжи Telegram.", nil)
			return
		}
		plan := strings.ToLower(strings.TrimSpace(user.Plan))
		if plan == "" {
			plan = "free"
		}
		if (slot == "SLOT_2" || slot == "SLOT_3") && plan == "free" {
			_ = a.sendMessage(chatID, "Команда недоступна для вашего плана. Нужен план Standard или Pro.", nil)
			return
		}
		if slot == "SLOT_3" && plan != "pro" {
			_ = a.sendMessage(chatID, "Команда недоступна для вашего плана. Нужен план Pro.", nil)
			return
		}
	}
	rows, err := a.store.UpdateScannerSlotEnabledByTelegramID(ctx, chatID, slot, enabled)
	if err != nil {
		_ = a.sendMessage(chatID, "Ошибка обновления сканера. Попробуй позже.", nil)
		return
	}
	if rows == 0 {
		_ = a.sendMessage(chatID, "Не найдено правил для этого сканера.", nil)
		return
	}
	a.sendStatusMessage(ctx, chatID)
}

func (a *API) sendProfileMessage(ctx context.Context, chatID int64) {
	user, err := a.store.GetUserByTelegramID(ctx, chatID)
	if err != nil {
		_ = a.sendMessage(chatID, "Профиль не найден. Перепривяжи Telegram.", nil)
		return
	}
	name := strings.TrimSpace(user.DisplayName)
	if name == "" {
		name = "User"
	}
	plan := strings.TrimSpace(user.Plan)
	if plan == "" {
		plan = "free"
	}
	created := user.CreatedAt.Format("02.01.2006 15:04")
	text := "👤 Профиль\n" +
		"Имя: " + escapeHTML(name) + "\n" +
		"ID: " + strconv.FormatInt(user.PublicID, 10) + "\n" +
		"План: " + strings.ToUpper(plan) + "\n" +
		"Дата регистрации: " + created
	_ = a.sendMessage(chatID, text, nil)
}

func escapeHTML(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
	)
	return replacer.Replace(value)
}

func (a *API) sendMessage(chatID int64, text string, replyMarkup any) error {
	if strings.TrimSpace(a.cfg.BotToken) == "" {
		return errors.New("bot_token_missing")
	}
	payload := sendMessageReq{
		ChatID:      chatID,
		Text:        text,
		ParseMode:   "HTML",
		ReplyMarkup: replyMarkup,
		DisableWeb:  true,
	}
	return a.post("sendMessage", payload)
}

func (a *API) answerCallback(callbackID, text string) error {
	payload := answerCallbackReq{
		CallbackID: callbackID,
		Text:       text,
		ShowAlert:  false,
	}
	return a.post("answerCallbackQuery", payload)
}

func (a *API) post(method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Post(a.methodURL(method), "application/json", bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram_api_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return errors.New("telegram_api_failed")
	}
	return nil
}

func (a *API) methodURL(method string) string {
	return strings.TrimRight(a.baseURL, "/") + "/bot" + a.cfg.BotToken + "/" + method
}
