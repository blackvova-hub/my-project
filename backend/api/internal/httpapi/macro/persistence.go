package macroapi

import (
	"context"
	"encoding/json"
	"log"
	"time"
)

func (a *API) loadPersistent() {
	if a.db == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var payload []byte
	err := a.db.QueryRow(ctx, `SELECT payload FROM news_macro_cache WHERE cache_key='terminal'`).Scan(&payload)
	if err != nil {
		return
	}
	var saved snapshot
	if json.Unmarshal(payload, &saved) != nil {
		return
	}
	for id, s := range saved.Series {
		if _, known := a.state.Series[id]; !known {
			continue
		}
		s.Points = normalizePoints(s.Points)
		if len(s.Points) > 0 && (s.LastUpdated == nil || s.Status != "fresh") {
			s.Status = "stale"
		} else if s.LastUpdated != nil {
			if at, e := time.Parse(time.RFC3339, *s.LastUpdated); e != nil || a.now().Sub(at) > macroInterval {
				s.Status = "stale"
			}
		}
		a.state.Series[id] = s
	}
	a.state.Events = deduplicateEvents(saved.Events)
	a.state.Markets = saved.Markets
	if saved.Sentiment != nil {
		item := *saved.Sentiment
		item.Points = normalizePoints(item.Points)
		if item.Price != nil && finite(*item.Price) && *item.Price >= 0 && *item.Price <= 100 && item.ObservedAt != nil {
			if at, e := time.Parse(time.RFC3339, *item.ObservedAt); e == nil {
				if a.now().Sub(at) > 48*time.Hour {
					item.Status = "stale"
				}
				a.state.Sentiment = &item
			}
		}
	}
	for i := range a.state.Markets {
		m := &a.state.Markets[i]
		if m.ObservedAt != nil {
			if at, e := time.Parse(time.RFC3339, *m.ObservedAt); e != nil || a.now().Sub(at) > marketInterval {
				m.Status = "stale"
			}
		} else {
			m.Status = "unavailable"
		}
	}
	for id, h := range saved.Sources {
		if _, known := a.state.Sources[id]; !known {
			continue
		}
		if h.LastSuccess != nil {
			interval := calendarInterval
			if id == "fred" || id == "bls-data" || id == "ecb" {
				interval = macroInterval
			}
			if id == "crypto-markets" {
				interval = marketInterval
			}
			if at, e := time.Parse(time.RFC3339, *h.LastSuccess); e != nil || a.now().Sub(at) > interval {
				h.Status = "stale"
			}
		}
		a.state.Sources[id] = h
		// Preserve request budgets across process restarts. Clients cannot
		// force API requests by refreshing the terminal page.
		if h.LastAttempt != nil {
			if at, e := time.Parse(time.RFC3339, *h.LastAttempt); e == nil {
				a.lastRun[id] = at
			}
		}
	}
}

func (a *API) persist(ctx context.Context) {
	if a.db == nil || ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	s := a.clone()
	payload, err := json.Marshal(s)
	if err != nil {
		return
	}
	_, err = a.db.Exec(ctx, `INSERT INTO news_macro_cache(cache_key,payload,updated_at) VALUES('terminal',$1::jsonb,now()) ON CONFLICT(cache_key) DO UPDATE SET payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, payload)
	if err != nil {
		log.Printf("macro: snapshot persistence unavailable")
		return
	}
	for _, series := range s.Series {
		if len(series.Points) == 0 || series.LastUpdated == nil || a.lastPersisted[series.ID] == *series.LastUpdated {
			continue
		}
		points, err := json.Marshal(series.Points)
		if err != nil {
			continue
		}
		// Upserts are bounded to the fixed catalog. Only changed observations
		// produce revisions; repeated refreshes do not grow history.
		_, err = a.db.Exec(ctx, `WITH incoming AS (
		  SELECT $1::text AS series_id, p.date::date AS observed_on,p.value::double precision AS value
		  FROM jsonb_to_recordset($2::jsonb) AS p(date text,value double precision)
		), revisions AS (
		  INSERT INTO news_macro_revisions(series_id,observed_on,previous_value,revised_value,detected_at)
		  SELECT old.series_id,old.observed_on,old.value,new.value,now()
		  FROM news_macro_observations old JOIN incoming new USING(series_id,observed_on)
		  WHERE old.value IS DISTINCT FROM new.value RETURNING series_id
		)
		INSERT INTO news_macro_observations(series_id,observed_on,value,source,source_url,fetched_at)
		SELECT series_id,observed_on,value,$3,$4,now() FROM incoming
		ON CONFLICT(series_id,observed_on) DO UPDATE SET value=EXCLUDED.value,source=EXCLUDED.source,source_url=EXCLUDED.source_url,fetched_at=EXCLUDED.fetched_at
		WHERE news_macro_observations.value IS DISTINCT FROM EXCLUDED.value
		   OR news_macro_observations.source IS DISTINCT FROM EXCLUDED.source`, series.ID, points, series.Source, series.SourceURL)
		if err != nil {
			log.Printf("macro: observation persistence unavailable")
			break
		}
		a.lastPersisted[series.ID] = *series.LastUpdated
	}
}
