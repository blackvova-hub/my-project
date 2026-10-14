package macroapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (a *API) refreshFRED(ctx context.Context) {
	ids := []string{}
	for _, s := range catalog() {
		if !strings.HasPrefix(s.ID, "ECB_") {
			ids = append(ids, s.ID)
		}
	}
	var firstErr error
	if a.fredKey != "" {
		// Serialized requests at <1/s respect the documented API limit; the key
		// is never included in returned metadata, errors, or request logs.
		for i, id := range ids {
			if i > 0 {
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
			u := "https://api.stlouisfed.org/fred/series/observations?" + url.Values{"series_id": {id}, "api_key": {a.fredKey}, "file_type": {"json"}}.Encode()
			data, err := a.get(ctx, u)
			var points []Point
			if err == nil {
				points, err = parseFREDJSON(data)
			}
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				a.staleSeries([]string{id})
				continue
			}
			s := a.clone().Series[id]
			a.applyPoints(id, points, s.Source, s.SourceURL)
		}
	} else {
		// FRED Graph limits downloads to 12 lines and returns a ZIP when
		// series have different frequencies. Two fixed requests every six
		// hours cover this catalog without discovery or crawling.
		for start := 0; start < len(ids); start += 12 {
			end := start + 12
			if end > len(ids) {
				end = len(ids)
			}
			batch := ids[start:end]
			u := "https://fred.stlouisfed.org/graph/fredgraph.csv?" + url.Values{"id": {strings.Join(batch, ",")}}.Encode()
			data, err := a.get(ctx, u)
			var points map[string][]Point
			if err == nil {
				points, err = parseFREDDownload(data)
			}
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				a.staleSeries(batch)
				continue
			}
			for _, id := range batch {
				if len(points[id]) == 0 {
					a.staleSeries([]string{id})
					if firstErr == nil {
						firstErr = errors.New("Часть рядов не содержит наблюдений")
					}
					continue
				}
				s := a.clone().Series[id]
				a.applyPoints(id, points[id], s.Source, s.SourceURL)
			}
		}
	}
	a.health("fred", firstErr)
	if firstErr != nil {
		a.refreshBLSData(ctx)
	}
	if firstErr == nil {
		a.mu.Lock()
		h := a.state.Sources["bls-data"]
		h.Status = "standby"
		h.Message = "Резерв не запрашивался: данные BLS доступны через FRED"
		a.state.Sources[h.ID] = h
		a.mu.Unlock()
	}
}

func parseFREDDownload(data []byte) (map[string][]Point, error) {
	if !bytes.HasPrefix(data, []byte{'P', 'K', 3, 4}) {
		return parseFREDCSV(data)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(z.File) > 20 {
		return nil, errors.New("FRED вернул неверный ZIP")
	}
	result := map[string][]Point{}
	var expanded uint64
	for _, f := range z.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		expanded += f.UncompressedSize64
		if expanded > maxResponseBytes {
			return nil, errors.New("Распакованные данные FRED превышают допустимый размер")
		}
		reader, e := f.Open()
		if e != nil {
			return nil, errors.New("Не удалось прочитать архив FRED")
		}
		body, e := io.ReadAll(io.LimitReader(reader, maxResponseBytes+1))
		reader.Close()
		if e != nil || len(body) > maxResponseBytes {
			return nil, errors.New("Не удалось распаковать FRED CSV")
		}
		series, e := parseFREDCSV(body)
		if e != nil {
			return nil, e
		}
		for id, points := range series {
			result[id] = normalizePoints(append(result[id], points...))
		}
	}
	if len(result) == 0 {
		return nil, errors.New("FRED ZIP не содержит наблюдений")
	}
	return result, nil
}

func parseFREDCSV(data []byte) (map[string][]Point, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil || len(head) < 2 || (!strings.EqualFold(head[0], "DATE") && !strings.EqualFold(head[0], "observation_date")) {
		return nil, errors.New("FRED вернул неверный формат CSV")
	}
	out := map[string][]Point{}
	for _, id := range head[1:] {
		out[id] = []Point{}
	}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("Не удалось разобрать FRED CSV")
		}
		if len(row) != len(head) {
			continue
		}
		if _, e = time.Parse("2006-01-02", row[0]); e != nil {
			continue
		}
		for i := 1; i < len(row); i++ {
			v, e := strconv.ParseFloat(row[i], 64)
			if e != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			out[head[i]] = append(out[head[i]], Point{row[0], v})
		}
	}
	count := 0
	for id, p := range out {
		out[id] = normalizePoints(p)
		count += len(out[id])
	}
	if count == 0 {
		return nil, errors.New("FRED не вернул числовых наблюдений")
	}
	return out, nil
}
func parseFREDJSON(data []byte) ([]Point, error) {
	var raw struct {
		Observations []struct {
			Date  string `json:"date"`
			Value string `json:"value"`
		} `json:"observations"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil, errors.New("FRED вернул неверный JSON")
	}
	points := []Point{}
	for _, p := range raw.Observations {
		v, e := strconv.ParseFloat(p.Value, 64)
		if e == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
			points = append(points, Point{p.Date, v})
		}
	}
	points = normalizePoints(points)
	if len(points) == 0 {
		return nil, errors.New("FRED не вернул наблюдений")
	}
	return points, nil
}

func (a *API) refreshBLSData(ctx context.Context) {
	// A single bundled query every six hours (four/day) fits the unregistered
	// quota of 25/day and ten years/query. The API may lag the news release.
	now := a.now()
	payload := map[string]any{"seriesid": []string{"CUSR0000SA0", "CUSR0000SA0L1E", "LNS14000000", "CES0000000001"}, "startyear": strconv.Itoa(now.Year() - 9), "endyear": strconv.Itoa(now.Year())}
	if a.blsKey != "" {
		payload["registrationkey"] = a.blsKey
	}
	encoded, _ := json.Marshal(payload)
	data, err := a.request(ctx, http.MethodPost, "https://api.bls.gov/publicAPI/v2/timeseries/data/", bytes.NewReader(encoded))
	var observations map[string][]Point
	if err == nil {
		observations, err = parseBLS(data)
	}
	ids := map[string]string{"CUSR0000SA0": "CPIAUCSL", "CUSR0000SA0L1E": "CPILFESL", "LNS14000000": "UNRATE", "CES0000000001": "PAYEMS"}
	if err == nil {
		for sourceID, id := range ids {
			if len(observations[sourceID]) > 0 {
				a.applyPoints(id, observations[sourceID], "BLS · Public Data API", "https://data.bls.gov/timeseries/"+sourceID)
			}
		}
	}
	a.health("bls-data", err)
}
func parseBLS(data []byte) (map[string][]Point, error) {
	var raw struct {
		Status  string `json:"status"`
		Results struct {
			Series []struct {
				ID   string `json:"seriesID"`
				Data []struct {
					Year   string `json:"year"`
					Period string `json:"period"`
					Value  string `json:"value"`
				} `json:"data"`
			} `json:"series"`
		} `json:"Results"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Status != "REQUEST_SUCCEEDED" {
		return nil, errors.New("BLS не вернул успешный ответ")
	}
	result := map[string][]Point{}
	for _, s := range raw.Results.Series {
		points := []Point{}
		for _, p := range s.Data {
			if len(p.Period) != 3 || p.Period[0] != 'M' {
				continue
			}
			month, e := strconv.Atoi(p.Period[1:])
			if e != nil || month < 1 || month > 12 {
				continue
			}
			value, e := strconv.ParseFloat(p.Value, 64)
			if e != nil || math.IsNaN(value) || math.IsInf(value, 0) {
				continue
			}
			points = append(points, Point{p.Year + "-" + fmt.Sprintf("%02d", month) + "-01", value})
		}
		result[s.ID] = normalizePoints(points)
	}
	count := 0
	for _, p := range result {
		count += len(p)
	}
	if count == 0 {
		return nil, errors.New("BLS не вернул наблюдений")
	}
	return result, nil
}

func (a *API) refreshECB(ctx context.Context) {
	var firstErr error
	for _, d := range []struct{ id, path, url string }{{"ECB_DFR", "FM/B.U2.EUR.4F.KR.DFR.LEV", "https://data.ecb.europa.eu/data/datasets/FM/FM.B.U2.EUR.4F.KR.DFR.LEV"}, {"ECB_EURUSD", "EXR/D.USD.EUR.SP00.A", "https://data.ecb.europa.eu/data/datasets/EXR/EXR.D.USD.EUR.SP00.A"}} {
		data, err := a.get(ctx, "https://data-api.ecb.europa.eu/service/data/"+d.path+"?format=csvdata")
		var points []Point
		if err == nil {
			points, err = parseECBCSV(data)
		}
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			a.staleSeries([]string{d.id})
			continue
		}
		a.applyPoints(d.id, points, "ECB", d.url)
	}
	a.health("ecb", firstErr)
}
func parseECBCSV(data []byte) ([]Point, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return nil, errors.New("ECB вернул неверный CSV")
	}
	dateCol, valueCol := -1, -1
	for i, h := range head {
		if h == "TIME_PERIOD" {
			dateCol = i
		}
		if h == "OBS_VALUE" {
			valueCol = i
		}
	}
	if dateCol < 0 || valueCol < 0 {
		return nil, errors.New("В ECB CSV отсутствуют TIME_PERIOD / OBS_VALUE")
	}
	points := []Point{}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("Не удалось разобрать ECB CSV")
		}
		if dateCol >= len(row) || valueCol >= len(row) {
			continue
		}
		value, e := strconv.ParseFloat(row[valueCol], 64)
		if e == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
			points = append(points, Point{row[dateCol], value})
		}
	}
	points = normalizePoints(points)
	if len(points) == 0 {
		return nil, errors.New("ECB не вернул наблюдений")
	}
	return points, nil
}
