package macroapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const blsCalendarURL = "https://www.bls.gov/schedule/news_release/bls.ics"
const beaCalendarURL = "https://www.bea.gov/news/schedule/ics/online-calendar-subscription.ics"
const fedCalendarURL = "https://www.federalreserve.gov/monetarypolicy/fomccalendars.htm"

func (a *API) refreshICS(ctx context.Context, id, sourceURL, source string) {
	data, err := a.get(ctx, sourceURL)
	var events []Event
	if err == nil {
		events, err = parseICS(data, source, sourceURL, a.now())
	}
	if err == nil {
		a.applyEvents(source, events)
	}
	a.health(id, err)
}

// parseICS unfolds RFC 5545 lines and honors UTC/TZID/date-only values. Floating
// BLS/BEA release times use the source's declared calendar timezone, Eastern
// time when it is omitted. Unknown timezone names are rejected, never guessed.
func parseICS(data []byte, source, sourceURL string, now time.Time) ([]Event, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.Contains(text, "BEGIN:VCALENDAR") {
		return nil, errors.New("Источник вернул неверный iCalendar")
	}
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
		} else {
			lines = append(lines, line)
		}
	}
	defaultTZ := "America/New_York"
	inEvent := false
	props := map[string]string{}
	events := []Event{}
	for _, line := range lines {
		if line == "BEGIN:VEVENT" {
			inEvent = true
			props = map[string]string{}
			continue
		}
		if line == "END:VEVENT" {
			inEvent = false
			e, err := icsEvent(props, source, sourceURL, defaultTZ, now)
			if err == nil {
				events = append(events, e)
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !inEvent && key == "X-WR-TIMEZONE" {
			defaultTZ = value
		}
		if inEvent {
			props[key] = value
		}
	}
	if len(events) == 0 {
		return nil, errors.New("Календарь не содержит событий с достоверной датой")
	}
	return deduplicateEvents(events), nil
}

func icsEvent(props map[string]string, source, sourceURL, defaultTZ string, now time.Time) (Event, error) {
	title := icsUnescape(props["SUMMARY"])
	if title == "" {
		return Event{}, errors.New("Missing summary")
	}
	start, tz, dateOnly := "", defaultTZ, false
	for k, v := range props {
		if k == "DTSTART" || strings.HasPrefix(k, "DTSTART;") {
			start = v
			for _, part := range strings.Split(k, ";")[1:] {
				if strings.HasPrefix(part, "TZID=") {
					tz = strings.Trim(strings.TrimPrefix(part, "TZID="), "\"")
				}
				if part == "VALUE=DATE" {
					dateOnly = true
				}
			}
		}
	}
	if start == "" {
		return Event{}, errors.New("Missing DTSTART")
	}
	if len(start) == 8 {
		dateOnly = true
	}
	e := classifyEvent(title, source, sourceURL)
	e.TimePrecision = "exact"
	e.Status = "upcoming"
	var at time.Time
	var err error
	if dateOnly {
		at, err = time.Parse("20060102", start)
		e.TimePrecision = "date"
	} else if strings.HasSuffix(start, "Z") {
		at, err = time.Parse("20060102T150405Z", start)
	} else {
		// BLS's RFC5545 VTIMEZONE uses the non-IANA identifier US-Eastern
		// with modern US daylight-saving rules. Map this declared alias to
		// the equivalent IANA timezone rather than a fixed UTC offset.
		if tz == "US-Eastern" {
			tz = "America/New_York"
		}
		location, e1 := time.LoadLocation(tz)
		if e1 != nil {
			return Event{}, e1
		}
		at, err = time.ParseInLocation("20060102T150405", start, location)
	}
	if err != nil {
		return Event{}, err
	}
	e.Date = at.Format("2006-01-02")
	if !dateOnly {
		e.ScheduledAt = ptr(at.UTC().Format(time.RFC3339))
	}
	if (!dateOnly && !at.After(now)) || (dateOnly && e.Date < now.UTC().Format("2006-01-02")) {
		e.Status = "elapsed"
	}
	if strings.EqualFold(props["STATUS"], "CANCELLED") {
		e.Status = "cancelled"
	}
	uid := props["UID"]
	if uid == "" {
		uid = title + "|" + start
	}
	sum := sha256.Sum256([]byte(source + "|" + uid))
	e.ID = strings.ToLower(source) + "-" + hex.EncodeToString(sum[:10])
	description := icsUnescape(props["DESCRIPTION"])
	if description != "" && description != "www.bea.gov" {
		e.Description += " " + description
	}
	return e, nil
}
func icsUnescape(s string) string {
	return strings.NewReplacer("\\n", "\n", "\\N", "\n", "\\,", ",", "\\;", ";", "\\\\", "\\").Replace(s)
}

var monthReferenceRE = regexp.MustCompile(`(?i)\b(January|February|March|April|May|June|July|August|September|October|November|December)\s+(20\d{2})\b`)
var quarterReferenceRE = regexp.MustCompile(`(?i)\b([1-4])(?:st|nd|rd|th)\s+Quarter(?:\s+and\s+Year)?\s+(20\d{2})\b`)

func classifyEvent(title, source, sourceURL string) Event {
	e := Event{Title: title, Description: "Официальная дата публикации. Прогноз консенсуса не предоставлен источником; отсутствующие значения показаны как нет данных.", Country: "US", Currency: "USD", Category: "other", Importance: "low", ImportanceReason: "Плановая статистическая публикация; повышенная рыночная значимость не установлена.", Unit: "", Source: source, SourceURL: sourceURL}
	lower := strings.ToLower(title)
	switch {
	case strings.Contains(lower, "consumer price index"):
		e.Category = "inflation"
		e.Importance = "high"
		e.SeriesID = ptr("CPIAUCSL")
		e.Unit = "индекс 1982–84 = 100"
		e.Description += " CPI измеряет уровень потребительских цен. Связанный график показывает индекс CPI, а не процент инфляции."
	case strings.Contains(lower, "producer price index"):
		e.Category = "inflation"
		e.Importance = "medium"
		e.Description += " PPI измеряет цены производителей; их изменения могут передаваться в потребительскую инфляцию."
	case strings.Contains(lower, "employment situation"):
		e.Category = "labor"
		e.Importance = "high"
		e.SeriesID = ptr("UNRATE")
		e.Unit = "%"
		e.Description += " Отчёт включает занятость и безработицу. Связанный показатель — уровень безработицы, а не изменение NFP."
	case strings.Contains(lower, "job openings"):
		e.Category = "labor"
		e.Importance = "medium"
		e.Description += " JOLTS отражает спрос на работников и напряжённость рынка труда."
	case strings.Contains(lower, "gross domestic product") || strings.HasPrefix(lower, "gdp ("):
		e.Category = "growth"
		e.Importance = "high"
		e.SeriesID = ptr("A191RL1Q225SBEA")
		e.Unit = "% годовых"
		e.Description += " ВВП измеряет выпуск экономики; предварительная, вторая и третья оценки относятся к одному кварталу и могут различаться."
	case strings.Contains(lower, "personal income and outlays"):
		e.Category = "inflation"
		e.Importance = "high"
		e.SeriesID = ptr("PCEPI")
		e.Unit = "индекс 2017 = 100"
		e.Description += " Отчёт включает доходы, потребление и PCE. Связанный график — индекс цен PCE."
	case strings.Contains(lower, "international trade"):
		e.Category = "growth"
		e.Importance = "medium"
		e.Description += " Торговый баланс показывает разницу экспорта и импорта товаров и услуг."
	case strings.Contains(lower, "productivity"):
		e.Category = "labor"
		e.Importance = "medium"
	}
	if e.Importance == "high" {
		e.ImportanceReason = "Базовый показатель инфляции, занятости или роста: может изменить ожидания денежной политики. Это редакционное правило, а не измеренная вероятность движения цены."
	} else if e.Importance == "medium" {
		e.ImportanceReason = "Дополнительный показатель экономики, влияющий на оценку роста или инфляции; значимость назначена по типу публикации."
	}
	if m := monthReferenceRE.FindStringSubmatch(title); len(m) > 0 {
		month, _ := time.Parse("January", strings.Title(strings.ToLower(m[1])))
		e.ReferencePeriod = ptr(fmt.Sprintf("%s-%02d", m[2], month.Month()))
	}
	if m := quarterReferenceRE.FindStringSubmatch(title); len(m) > 0 {
		e.ReferencePeriod = ptr(m[2] + "-Q" + m[1])
	}
	return e
}

func deduplicateEvents(events []Event) []Event {
	byID := map[string]Event{}
	byTitle := map[string]string{}
	for _, e := range events {
		key := strings.ToLower(e.Source + "|" + strings.Join(strings.Fields(e.Title), " ") + "|" + e.Date)
		if id, ok := byTitle[key]; ok {
			previous := byID[id]
			if previous.TimePrecision == "date" && e.TimePrecision == "exact" {
				delete(byID, id)
			} else {
				continue
			}
		}
		byID[e.ID] = e
		byTitle[key] = e.ID
	}
	out := make([]Event, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out
}

func (a *API) applyEvents(source string, events []Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := map[string]Event{}
	keep := []Event{}
	incoming := map[string]bool{}
	for _, e := range events {
		incoming[e.ID] = true
	}
	for _, e := range a.state.Events {
		old[e.ID] = e
		if e.Source != source {
			keep = append(keep, e)
		} else if !incoming[e.ID] && e.Date < a.now().UTC().Format("2006-01-02") {
			// Rolling calendars can remove past releases. Preserve observed
			// schedules without inventing first-release values or retaining
			// withdrawn future appointments.
			keep = append(keep, e)
		}
	}
	for i, e := range events {
		if prior, ok := old[e.ID]; ok && prior.PreviousScheduledAt != nil {
			e.PreviousScheduledAt = prior.PreviousScheduledAt
		}
		if prior, ok := old[e.ID]; ok && prior.ScheduledAt != nil && e.ScheduledAt != nil && *prior.ScheduledAt != *e.ScheduledAt {
			e.PreviousScheduledAt = prior.ScheduledAt
			if e.Status == "upcoming" {
				e.Status = "postponed"
			}
		}
		events[i] = e
	}
	a.state.Events = deduplicateEvents(append(keep, events...))
}

func (a *API) refreshFOMC(ctx context.Context) {
	data, err := a.get(ctx, fedCalendarURL)
	var events []Event
	if err == nil {
		events, err = parseFOMC(data, a.now())
	}
	if err == nil {
		a.applyEvents("Federal Reserve", events)
	}
	a.health("fed-calendar", err)
}

var fomcYearRE = regexp.MustCompile(`\b(20\d{2}) FOMC Meetings\b`)
var fomcDaysRE = regexp.MustCompile(`\d{1,2}`)

func parseFOMC(data []byte, now time.Time) ([]Event, error) {
	doc, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("Не удалось прочитать FOMC calendar")
	}
	year := 0
	events := []Event{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "h4" {
			if m := fomcYearRE.FindStringSubmatch(nodeText(n)); len(m) > 0 {
				year, _ = strconv.Atoi(m[1])
			}
		}
		if n.Type == html.ElementNode && hasClass(n, "fomc-meeting") && year > 0 {
			monthText, dayText := "", ""
			var find func(*html.Node)
			find = func(c *html.Node) {
				if hasClass(c, "fomc-meeting__month") {
					monthText = nodeText(c)
				}
				if hasClass(c, "fomc-meeting__date") {
					dayText = nodeText(c)
				}
				for child := c.FirstChild; child != nil; child = child.NextSibling {
					find(child)
				}
			}
			find(n)
			// Cross-month meetings (e.g. April/May) need an explicit second
			// month. The final day is the decision date, no intraday time is
			// asserted because the calendar page does not supply one.
			monthNames := strings.Split(strings.TrimSpace(monthText), "/")
			monthName := strings.TrimSpace(monthNames[len(monthNames)-1])
			month, me := time.Parse("January", monthName)
			days := fomcDaysRE.FindAllString(dayText, -1)
			if me == nil && len(days) > 0 {
				day, _ := strconv.Atoi(days[len(days)-1])
				at := time.Date(year, month.Month(), day, 0, 0, 0, 0, time.UTC)
				if at.Month() == month.Month() {
					date := at.Format("2006-01-02")
					status := "upcoming"
					if date < now.UTC().Format("2006-01-02") {
						status = "elapsed"
					}
					events = append(events, Event{ID: "fomc-" + date, Title: "FOMC · решение по денежной политике", Description: "Последний день заседания FOMC из официального календаря. Календарь не содержит точного времени решения: обратный отсчёт до минуты недоступен. Целевой диапазон ставки и эффективная ставка — разные показатели. Прогноз не предоставлен.", Country: "US", Currency: "USD", Category: "rates", Importance: "high", ImportanceReason: "Решение центрального банка влияет на стоимость денег и ожидания ставок. Назначено по типу события.", Date: date, TimePrecision: "date", Status: status, Unit: "%", SeriesID: ptr("DFEDTARU"), Source: "Federal Reserve", SourceURL: fedCalendarURL})
				}
			}
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if len(events) == 0 {
		return nil, errors.New("FOMC calendar не содержит распознаваемых дат")
	}
	return deduplicateEvents(events), nil
}
func hasClass(n *html.Node, name string) bool {
	for _, a := range n.Attr {
		if a.Key == "class" {
			for _, s := range strings.Fields(a.Val) {
				if s == name {
					return true
				}
			}
		}
	}
	return false
}
func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
			b.WriteString(" ")
		}
		for child := c.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return strings.Join(strings.Fields(b.String()), " ")
}
