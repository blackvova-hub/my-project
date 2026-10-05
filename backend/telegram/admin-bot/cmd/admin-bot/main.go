package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	telegramAPIBase = "https://api.telegram.org"
	// Ubuntu 26.04 Docker requires API v1.44 or newer. Keep this at the
	// server's documented minimum rather than negotiating through a shell.
	dockerAPIBase   = "http://docker/v1.44"
	maxMessageRunes = 3900
)

type config struct {
	Token           string
	BootstrapSecret string
	APIBase         string
	DockerSocket    string
	StatePath       string
	HostRoot        string
	ReportPath      string
	PollTimeout     time.Duration
	InitialChats    []int64
}

func loadConfig() (config, error) {
	cfg := config{
		Token:           strings.TrimSpace(os.Getenv("ADMIN_BOT_TOKEN")),
		BootstrapSecret: strings.TrimSpace(os.Getenv("ADMIN_BOT_BOOTSTRAP_SECRET")),
		APIBase:         env("ADMIN_BOT_API_BASE_URL", telegramAPIBase),
		DockerSocket:    env("ADMIN_BOT_DOCKER_SOCKET", "/var/run/docker.sock"),
		StatePath:       env("ADMIN_BOT_STATE_PATH", "/state/authorized-chats.json"),
		HostRoot:        env("ADMIN_BOT_HOST_ROOT", "/host"),
		ReportPath:      env("ADMIN_BOT_HEALTH_REPORT", "/reports/server-health-latest.log"),
		PollTimeout:     durationEnv("ADMIN_BOT_POLL_TIMEOUT", 25*time.Second),
		InitialChats:    parseChatIDs(os.Getenv("ADMIN_BOT_ALLOWED_CHAT_IDS")),
	}
	if cfg.Token == "" {
		return config{}, errors.New("ADMIN_BOT_TOKEN is required")
	}
	if len(cfg.BootstrapSecret) < 16 {
		return config{}, errors.New("ADMIN_BOT_BOOTSTRAP_SECRET must have at least 16 characters")
	}
	if cfg.PollTimeout < 5*time.Second || cfg.PollTimeout > 50*time.Second {
		return config{}, errors.New("ADMIN_BOT_POLL_TIMEOUT must be between 5s and 50s")
	}
	return cfg, nil
}

type authorizationState struct {
	BootstrapClaimed bool    `json:"bootstrap_claimed"`
	ChatIDs          []int64 `json:"chat_ids"`
}

type authorizer struct {
	mu    sync.Mutex
	path  string
	state authorizationState
}

func openAuthorizer(path string, initial []int64) (*authorizer, error) {
	a := &authorizer{path: path}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read authorization state: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &a.state); err != nil {
			return nil, fmt.Errorf("decode authorization state: %w", err)
		}
	}
	for _, id := range initial {
		a.add(id)
	}
	if len(initial) > 0 {
		if err := a.saveLocked(); err != nil {
			return nil, err
		}
	}
	return a, nil
}

func (a *authorizer) allowed(chatID int64) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, id := range a.state.ChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func (a *authorizer) claim(chatID int64, secret string, expected string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.state.BootstrapClaimed {
		return errors.New("bootstrap access has already been claimed")
	}
	if subtle.ConstantTimeCompare([]byte(secret), []byte(expected)) != 1 {
		return errors.New("invalid bootstrap secret")
	}
	a.add(chatID)
	a.state.BootstrapClaimed = true
	return a.saveLocked()
}

func (a *authorizer) grant(actor, chatID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.allowedLocked(actor) {
		return errors.New("not authorized")
	}
	a.add(chatID)
	return a.saveLocked()
}

func (a *authorizer) revoke(actor, chatID int64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.allowedLocked(actor) {
		return errors.New("not authorized")
	}
	if actor == chatID && len(a.state.ChatIDs) == 1 {
		return errors.New("refusing to remove the last administrator")
	}
	next := a.state.ChatIDs[:0]
	for _, id := range a.state.ChatIDs {
		if id != chatID {
			next = append(next, id)
		}
	}
	a.state.ChatIDs = next
	return a.saveLocked()
}

func (a *authorizer) add(chatID int64) {
	if chatID == 0 || a.allowedLocked(chatID) {
		return
	}
	a.state.ChatIDs = append(a.state.ChatIDs, chatID)
	sort.Slice(a.state.ChatIDs, func(i, j int) bool { return a.state.ChatIDs[i] < a.state.ChatIDs[j] })
}

func (a *authorizer) allowedLocked(chatID int64) bool {
	for _, id := range a.state.ChatIDs {
		if id == chatID {
			return true
		}
	}
	return false
}

func (a *authorizer) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(a.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(a.state)
	if err != nil {
		return err
	}
	temporary := a.path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	return os.Rename(temporary, a.path)
}

type telegramClient struct {
	token  string
	base   string
	client *http.Client
}

type telegramResult struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type telegramUpdate struct {
	UpdateID int64 `json:"update_id"`
	Message  *struct {
		Text string `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

func (t *telegramClient) call(ctx context.Context, method string, input any, output any) error {
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(t.base, "/")+"/bot"+t.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	var result telegramResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("Telegram returned invalid JSON: %w", err)
	}
	if response.StatusCode/100 != 2 || !result.OK {
		if result.Description == "" {
			result.Description = response.Status
		}
		return fmt.Errorf("Telegram %s: %s", method, result.Description)
	}
	if output != nil && len(result.Result) > 0 {
		return json.Unmarshal(result.Result, output)
	}
	return nil
}

func (t *telegramClient) updates(ctx context.Context, offset int64, timeout time.Duration) ([]telegramUpdate, error) {
	var updates []telegramUpdate
	err := t.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": int(timeout / time.Second), "allowed_updates": []string{"message"}}, &updates)
	return updates, err
}

func (t *telegramClient) send(ctx context.Context, chatID int64, text string) error {
	for _, part := range splitMessage(redact(text), maxMessageRunes) {
		if err := t.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": part, "disable_web_page_preview": true}, nil); err != nil {
			return err
		}
	}
	return nil
}

type dockerClient struct{ client *http.Client }

func newDockerClient(socket string) *dockerClient {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		},
		DisableCompression:    true,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &dockerClient{client: &http.Client{Transport: transport, Timeout: 20 * time.Second}}
}

func (d *dockerClient) request(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, dockerAPIBase+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode/100 != 2 {
		return nil, fmt.Errorf("Docker %s %s: %s", method, path, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

type dockerInspect struct {
	Name         string `json:"Name"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status    string `json:"Status"`
		OOMKilled bool   `json:"OOMKilled"`
		ExitCode  int    `json:"ExitCode"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

func (d *dockerClient) containers(ctx context.Context) ([]dockerContainer, error) {
	raw, err := d.request(ctx, http.MethodGet, "/containers/json?all=1", nil)
	if err != nil {
		return nil, err
	}
	var containers []dockerContainer
	return containers, json.Unmarshal(raw, &containers)
}

func (d *dockerClient) inspect(ctx context.Context, name string) (dockerInspect, error) {
	raw, err := d.request(ctx, http.MethodGet, "/containers/"+url.PathEscape(name)+"/json", nil)
	if err != nil {
		return dockerInspect{}, err
	}
	var inspection dockerInspect
	return inspection, json.Unmarshal(raw, &inspection)
}

func (d *dockerClient) logs(ctx context.Context, name string, tail int) (string, error) {
	path := fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&tail=%d&timestamps=1", url.PathEscape(name), tail)
	raw, err := d.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	return string(dockerFrames(raw)), nil
}

func (d *dockerClient) exec(ctx context.Context, name string, command ...string) (string, error) {
	raw, err := d.request(ctx, http.MethodPost, "/containers/"+url.PathEscape(name)+"/exec", map[string]any{"AttachStdout": true, "AttachStderr": true, "Cmd": command})
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil {
		return "", fmt.Errorf("Docker exec creation: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("Docker exec creation returned no ID")
	}
	raw, err = d.request(ctx, http.MethodPost, "/exec/"+url.PathEscape(created.ID)+"/start", map[string]any{"Detach": false, "Tty": false})
	if err != nil {
		return "", err
	}
	return string(dockerFrames(raw)), nil
}

func dockerFrames(raw []byte) []byte {
	var output bytes.Buffer
	for len(raw) >= 8 && raw[0] <= 2 && raw[1] == 0 && raw[2] == 0 && raw[3] == 0 {
		size := int(binary.BigEndian.Uint32(raw[4:8]))
		if size > len(raw)-8 {
			break
		}
		output.Write(raw[8 : 8+size])
		raw = raw[8+size:]
	}
	output.Write(raw)
	return output.Bytes()
}

type adminBot struct {
	cfg    config
	tg     *telegramClient
	docker *dockerClient
	auth   *authorizer
}

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check Docker socket and authorization state")
	flag.Parse()
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	auth, err := openAuthorizer(cfg.StatePath, cfg.InitialChats)
	if err != nil {
		log.Fatal(err)
	}
	bot := &adminBot{cfg: cfg, auth: auth, docker: newDockerClient(cfg.DockerSocket), tg: &telegramClient{token: cfg.Token, base: cfg.APIBase, client: &http.Client{Timeout: cfg.PollTimeout + 15*time.Second}}}
	if *healthcheck {
		if _, err := bot.docker.containers(context.Background()); err != nil {
			log.Fatal(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := bot.tg.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil); err != nil {
		log.Fatalf("remove webhook: %v", err)
	}
	log.Print("Telegram server admin bot started")
	bot.poll(ctx)
}

func (b *adminBot) poll(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		updates, err := b.tg.updates(ctx, offset, b.cfg.PollTimeout)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("Telegram poll: %v", err)
				wait(ctx, 3*time.Second)
			}
			continue
		}
		for _, update := range updates {
			offset = update.UpdateID + 1
			if update.Message == nil || update.Message.Chat.Type != "private" || strings.TrimSpace(update.Message.Text) == "" {
				continue
			}
			b.handle(ctx, update.Message.Chat.ID, update.Message.Text)
		}
	}
}

func (b *adminBot) handle(parent context.Context, chatID int64, message string) {
	parts := strings.Fields(message)
	if len(parts) == 0 {
		return
	}
	command := strings.ToLower(strings.Split(parts[0], "@")[0])
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	respond := func(text string) {
		if err := b.tg.send(ctx, chatID, text); err != nil && parent.Err() == nil {
			log.Printf("Telegram send chat=%d: %v", chatID, err)
		}
	}
	if command == "/start" || command == "/help" {
		respond(helpText(b.auth.allowed(chatID)))
		return
	}
	if command == "/id" {
		respond(fmt.Sprintf("Ваш Telegram chat ID: %d", chatID))
		return
	}
	if command == "/claim" {
		if len(parts) != 2 {
			respond("Формат: /claim <bootstrap-secret>")
			return
		}
		if err := b.auth.claim(chatID, parts[1], b.cfg.BootstrapSecret); err != nil {
			respond("Доступ не выдан: " + err.Error())
			return
		}
		log.Printf("admin chat linked id=%d", chatID)
		respond("Готово: этот чат привязан как администратор. /help — список команд.")
		return
	}
	if !b.auth.allowed(chatID) {
		respond("Нет доступа. Владелец должен сначала привязать этот чат.")
		return
	}
	switch command {
	case "/status":
		respond(b.status(ctx))
	case "/host":
		respond(b.hostReport())
	case "/archive":
		respond(b.archive(ctx))
	case "/containers":
		respond(b.containers(ctx))
	case "/temperature":
		respond(b.temperature())
	case "/disk":
		respond(b.disks())
	case "/errors":
		respond(b.errors(ctx))
	case "/logs":
		if len(parts) != 2 {
			respond("Формат: /logs archive|backend|clickhouse|postgres|redis|news|worker")
			return
		}
		respond(b.logs(ctx, strings.ToLower(parts[1])))
	case "/grant", "/revoke":
		if len(parts) != 2 {
			respond("Формат: " + command + " <numeric-chat-id>")
			return
		}
		id, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			respond("Некорректный chat ID.")
			return
		}
		if command == "/grant" {
			err = b.auth.grant(chatID, id)
		} else {
			err = b.auth.revoke(chatID, id)
		}
		if err != nil {
			respond("Не выполнено: " + err.Error())
			return
		}
		respond("Готово.")
	default:
		respond("Неизвестная команда. /help")
	}
}

func (b *adminBot) status(ctx context.Context) string {
	containers, err := b.docker.containers(ctx)
	if err != nil {
		return "Статус Docker недоступен: " + err.Error()
	}
	running, stopped := 0, 0
	for _, c := range containers {
		if c.State == "running" {
			running++
		} else {
			stopped++
		}
	}
	archive := b.archive(ctx)
	return strings.Join([]string{
		"Server01 — " + hostName(b.cfg.HostRoot),
		"Load: " + readFirst(b.cfg.HostRoot+"/proc/loadavg"),
		memorySummary(b.cfg.HostRoot + "/proc/meminfo"),
		"CPU temp: " + strings.Join(cpuTemperatures(b.cfg.HostRoot), ", "),
		filesystemSummary(b.cfg.HostRoot),
		fmt.Sprintf("Containers: running=%d stopped=%d", running, stopped),
		archive,
	}, "\n")
}

func (b *adminBot) hostReport() string {
	raw, err := os.ReadFile(b.cfg.ReportPath)
	if err != nil {
		return "Нет свежего host-отчёта: " + err.Error()
	}
	return "Полный host-отчёт:\n" + string(raw)
}

type archiveLine struct {
	Exchange           string `json:"exchange"`
	Market             string `json:"market"`
	Series             int    `json:"series"`
	HistoryBarsToCheck int64  `json:"historyBarsToCheck"`
	ProcessedRows      int64  `json:"processedRows"`
	Errors             int    `json:"errors"`
	MaxLiveLagSeconds  int64  `json:"maxLiveLagSeconds"`
	SourceMissingBars  int64  `json:"sourceMissingBars"`
}

func (b *adminBot) archive(ctx context.Context) string {
	raw, err := b.docker.exec(ctx, "shortlong-market_history-1", "/app/archive-sync", "-status")
	if err != nil {
		return "Архив: " + err.Error()
	}
	var rows []archiveLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &rows); err != nil {
		return "Архив вернул непонятный статус: " + truncate(strings.TrimSpace(raw), 500)
	}
	lines := []string{"Архив 1m:"}
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("%s %s: series=%d, rows=%d, осталось=%d, errors=%d, source gaps=%d, lag=%dm", row.Exchange, row.Market, row.Series, row.ProcessedRows, row.HistoryBarsToCheck, row.Errors, row.SourceMissingBars, row.MaxLiveLagSeconds/60))
	}
	return strings.Join(lines, "\n")
}

func (b *adminBot) containers(ctx context.Context) string {
	items, err := b.docker.containers(ctx)
	if err != nil {
		return "Контейнеры: " + err.Error()
	}
	sort.Slice(items, func(i, j int) bool { return firstName(items[i]) < firstName(items[j]) })
	lines := []string{"Контейнеры:"}
	for _, item := range items {
		inspection, err := b.docker.inspect(ctx, item.ID)
		if err != nil {
			lines = append(lines, firstName(item)+": "+item.Status)
			continue
		}
		health := ""
		if inspection.State.Health != nil {
			health = " health=" + inspection.State.Health.Status
		}
		lines = append(lines, fmt.Sprintf("%s: %s%s restarts=%d oom=%t", strings.TrimPrefix(inspection.Name, "/"), inspection.State.Status, health, inspection.RestartCount, inspection.State.OOMKilled))
	}
	return strings.Join(lines, "\n")
}

func (b *adminBot) temperature() string {
	temps := cpuTemperatures(b.cfg.HostRoot)
	if len(temps) == 0 {
		return "CPU temperature sensor unavailable."
	}
	return "CPU temperatures:\n" + strings.Join(temps, "\n")
}

func (b *adminBot) disks() string { return filesystemSummary(b.cfg.HostRoot) }

func (b *adminBot) errors(ctx context.Context) string {
	services := []string{"crypto_backend", "shortlong-market_history-1", "shortlong-clickhouse-1", "crypto_postgres", "crypto_redis", "crypto_news_bot", "crypto_worker", "shortlong-backtest_worker-1", "shortlong-similarity_worker-1"}
	lines := []string{"Recent warnings/errors:"}
	for _, service := range services {
		raw, err := b.docker.logs(ctx, service, 120)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(raw, "\n") {
			lower := strings.ToLower(line)
			if strings.Contains(lower, "turnstile_secret") || !(strings.Contains(lower, "warn") || strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic") || strings.Contains(lower, "exception")) {
				continue
			}
			lines = append(lines, "["+service+"] "+line)
			if len(lines) >= 28 {
				return strings.Join(lines, "\n")
			}
		}
	}
	if len(lines) == 1 {
		return "Recent warnings/errors: none in checked service tails."
	}
	return strings.Join(lines, "\n")
}

func (b *adminBot) logs(ctx context.Context, alias string) string {
	services := map[string]string{"archive": "shortlong-market_history-1", "backend": "crypto_backend", "clickhouse": "shortlong-clickhouse-1", "postgres": "crypto_postgres", "redis": "crypto_redis", "news": "crypto_news_bot", "worker": "crypto_worker"}
	service := services[alias]
	if service == "" {
		return "Недоступный сервис. Допустимо: archive, backend, clickhouse, postgres, redis, news, worker."
	}
	raw, err := b.docker.logs(ctx, service, 60)
	if err != nil {
		return "Логи: " + err.Error()
	}
	return "Logs " + service + ":\n" + strings.TrimSpace(raw)
}

func hostName(root string) string {
	if value := strings.TrimSpace(readFile(root + "/etc/hostname")); value != "" {
		return value
	}
	return "unknown"
}

func memorySummary(path string) string {
	values := map[string]int64{}
	for _, line := range strings.Split(readFile(path), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if number, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
			values[strings.TrimSuffix(fields[0], ":")] = number * 1024
		}
	}
	total, available := values["MemTotal"], values["MemAvailable"]
	if total == 0 {
		return "RAM: unavailable"
	}
	return fmt.Sprintf("RAM: used=%s, available=%s, total=%s", humanBytes(total-available), humanBytes(available), humanBytes(total))
}

func cpuTemperatures(root string) []string {
	paths, _ := filepath.Glob(root + "/sys/class/hwmon/hwmon*")
	lines := []string{}
	for _, path := range paths {
		if strings.TrimSpace(readFile(path+"/name")) != "coretemp" {
			continue
		}
		inputs, _ := filepath.Glob(path + "/temp*_input")
		for _, input := range inputs {
			value, err := strconv.ParseFloat(strings.TrimSpace(readFile(input)), 64)
			if err == nil {
				lines = append(lines, fmt.Sprintf("%s %.1f°C", filepath.Base(strings.TrimSuffix(input, "_input")), value/1000))
			}
		}
	}
	return lines
}

func filesystemSummary(root string) string {
	return strings.Join([]string{statfs(root, "SSD root"), statfs(root+"/srv/archive", "HDD archive")}, "\n")
}

func splitMessage(value string, limit int) []string {
	runes := []rune(value)
	if len(runes) <= limit {
		return []string{value}
	}
	parts := []string{}
	for len(runes) > 0 {
		n := limit
		if len(runes) < n {
			n = len(runes)
		}
		if n < len(runes) {
			for i := n; i > limit/2; i-- {
				if runes[i] == '\n' {
					n = i + 1
					break
				}
			}
		}
		parts = append(parts, string(runes[:n]))
		runes = runes[n:]
	}
	return parts
}

var sensitiveAssignment = regexp.MustCompile(`(?i)(password|token|secret|authorization)=([^\s&]+)`)

func redact(value string) string { return sensitiveAssignment.ReplaceAllString(value, "$1=[redacted]") }

func parseChatIDs(raw string) []int64 {
	ids := []int64{}
	for _, part := range strings.Split(raw, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id != 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

func helpText(authorized bool) string {
	if !authorized {
		return "Server admin bot. Private chats only. /id shows your chat ID. The owner can link the first chat with /claim <bootstrap-secret>."
	}
	return "Команды:\n/status — сводка\n/host — полный host-отчёт\n/archive — прогресс 1m архива\n/containers — Docker статус\n/temperature — CPU температуры\n/disk — место\n/errors — свежие ошибки\n/logs archive|backend|clickhouse|postgres|redis|news|worker\n/id, /grant <chat-id>, /revoke <chat-id>"
}

func firstName(container dockerContainer) string {
	if len(container.Names) == 0 {
		return container.ID
	}
	return strings.TrimPrefix(container.Names[0], "/")
}

func humanBytes(value int64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	result := float64(value)
	for _, unit := range units {
		result /= 1024
		if result < 1024 || unit == "TiB" {
			return fmt.Sprintf("%.1f %s", result, unit)
		}
	}
	return fmt.Sprintf("%d B", value)
}

func readFile(path string) string {
	value, _ := os.ReadFile(path)
	return string(value)
}

func readFirst(path string) string { return strings.TrimSpace(readFile(path)) }

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if value, err := time.ParseDuration(strings.TrimSpace(os.Getenv(key))); err == nil && value > 0 {
		return value
	}
	return fallback
}

func wait(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
