package similarity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrQueueFull = errors.New("Уже есть активный поиск или очередь заполнена. Дождитесь результата либо отмените поиск.")

type Job struct {
	ID       string    `json:"id"`
	Status   string    `json:"status"`
	Phase    string    `json:"phase"`
	Progress int       `json:"progress"`
	Request  Request   `json:"request"`
	Result   *Response `json:"result,omitempty"`
	Error    string    `json:"error,omitempty"`
	Allowed  []string  `json:"-"`
	Token    string    `json:"-"`
}
type Jobs struct {
	Search   *Searcher
	Pipeline Pipeline
	wake     chan struct{}
	mu       sync.Mutex
	running  map[string]context.CancelFunc
}

func NewJobs(search *Searcher, pipeline Pipeline) *Jobs {
	return &Jobs{Search: search, Pipeline: pipeline, wake: make(chan struct{}, 1), running: make(map[string]context.CancelFunc)}
}
func (j *Jobs) notify() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}

func (j *Jobs) Create(ctx context.Context, owner string, r Request) (string, error) {
	if err := j.Search.Store.SyncAssets(ctx); err != nil {
		return "", err
	}
	assets, err := j.Search.Store.Assets(ctx)
	if err != nil {
		return "", err
	}
	allowed := Allowed(assets, r)
	if len(allowed) == 0 {
		return "", errors.New("В выбранной области нет доступной истории")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	keyData, _ := json.Marshal([]any{r, allowed, j.Search.Store.Config.Version})
	hash := fmt.Sprintf("%x", sha256.Sum256(keyData))
	tx, err := j.Search.Store.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	// A short admission lock makes per-user/global limits and request coalescing atomic.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(714930662)`); err != nil {
		return "", err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM similarity_jobs WHERE user_id=$1 AND request_hash=$2 AND status IN ('queued','running','completed') AND created_at>now()-interval '1 hour' ORDER BY created_at DESC LIMIT 1`, owner, hash).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return "", err
	}
	var own, total int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE user_id=$1),count(*) FROM similarity_jobs WHERE status IN ('queued','running')`, owner).Scan(&own, &total); err != nil {
		return "", err
	}
	if own > 0 || total >= 32 {
		return "", ErrQueueFull
	}
	if _, err = tx.Exec(ctx, `DELETE FROM similarity_jobs WHERE status IN ('completed','failed','cancelled') AND created_at<now()-interval '7 days'`); err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, `INSERT INTO similarity_jobs(user_id,request_hash,request,allowed) VALUES($1,$2,$3,$4) RETURNING id::text`, owner, hash, raw, allowed).Scan(&id)
	if err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	j.notify()
	return id, nil
}
func (j *Jobs) Get(ctx context.Context, owner, id string) (Job, error) {
	var v Job
	var raw, result []byte
	err := j.Search.Store.DB.QueryRow(ctx, `SELECT id::text,status,phase,progress,request,result,error FROM similarity_jobs WHERE id=$1::uuid AND user_id=$2`, id, owner).Scan(&v.ID, &v.Status, &v.Phase, &v.Progress, &raw, &result, &v.Error)
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal(raw, &v.Request); err != nil {
		return v, err
	}
	if len(result) > 0 {
		err = json.Unmarshal(result, &v.Result)
	}
	return v, err
}
func (j *Jobs) Cancel(ctx context.Context, owner, id string) error {
	tag, err := j.Search.Store.DB.Exec(ctx, `UPDATE similarity_jobs SET status='cancelled',phase='Поиск отменён',finished_at=now(),lease_token=NULL,lease_until='-infinity' WHERE id=$1::uuid AND user_id=$2 AND status IN ('queued','running')`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		_, err = j.Get(ctx, owner, id)
		return err
	}
	j.mu.Lock()
	if cancel := j.running[id]; cancel != nil {
		cancel()
	}
	j.mu.Unlock()
	return nil
}
func (j *Jobs) claim(ctx context.Context) (*Job, error) {
	_, err := j.Search.Store.DB.Exec(ctx, `UPDATE similarity_jobs SET status='failed',phase='Поиск прерван',error='Не удалось восстановить задачу после нескольких остановок. Запустите поиск повторно.',finished_at=now() WHERE status='running' AND lease_until<now() AND attempts>=3`)
	if err != nil {
		return nil, err
	}
	var v Job
	var raw []byte
	err = j.Search.Store.DB.QueryRow(ctx, `UPDATE similarity_jobs SET status='running',phase='Подготовка поиска',lease_token=gen_random_uuid(),lease_until=now()+interval '1 minute',attempts=attempts+1 WHERE id=(SELECT id FROM similarity_jobs WHERE status='queued' OR (status='running' AND lease_until<now() AND attempts<3) ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id::text,request,allowed,lease_token::text`).Scan(&v.ID, &raw, &v.Allowed, &v.Token)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &v.Request)
	return &v, err
}
func (j *Jobs) touch(ctx context.Context, v Job, progress int, phase string) error {
	tag, err := j.Search.Store.DB.Exec(ctx, `UPDATE similarity_jobs SET progress=$3,phase=$4,lease_until=now()+interval '1 minute' WHERE id=$1::uuid AND lease_token=$2::uuid AND status='running'`, v.ID, v.Token, progress, phase)
	if err == nil && tag.RowsAffected() != 1 {
		return context.Canceled
	}
	return err
}
func (j *Jobs) process(ctx context.Context, v Job) (*Response, error) {
	s := j.Search.Store
	if q, ok := j.Search.Index.(*Qdrant); ok {
		w, _ := s.Config.Window(v.Request.Window)
		if err := q.Init(ctx, []Window{w}); err != nil {
			return nil, err
		}
	}
	// Creating coverage is scoped to this request; outcomes are computed for final matches only.
	if err := s.Discover(ctx, v.Request, v.Allowed); err != nil {
		return nil, err
	}
	total, err := s.Pending(ctx, v.Request, v.Allowed)
	if err != nil {
		return nil, err
	}
	for {
		remaining, err := s.Pending(ctx, v.Request, v.Allowed)
		if err != nil {
			return nil, err
		}
		progress := 90
		if total > 0 {
			progress = 5 + int(85*float64(total-remaining)/float64(total))
			progress = max(5, min(90, progress))
		}
		if err = j.touch(ctx, v, progress, "Подготовка выбранной истории"); err != nil {
			return nil, err
		}
		task, err := s.Claim(ctx, v.Request, v.Allowed)
		if err != nil {
			return nil, err
		}
		if task == nil {
			if remaining == 0 {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}
		work, cancel := context.WithTimeout(ctx, 2*time.Minute)
		missing, err := j.Pipeline.Process(work, *task)
		if err == nil {
			err = s.Finish(work, *task, missing)
		}
		cancel()
		if err != nil {
			release, done := context.WithTimeout(context.Background(), 5*time.Second)
			cause := err
			if ctx.Err() != nil {
				cause = nil
			}
			_ = s.Release(release, *task, cause)
			done()
			return nil, err
		}
	}
	if err = j.touch(ctx, v, 95, "Поиск совпадений и построение графиков"); err != nil {
		return nil, err
	}
	raw, err := j.Search.Search(ctx, v.Request)
	if err != nil {
		return nil, err
	}
	var result Response
	err = json.Unmarshal(raw, &result)
	return &result, err
}
func (j *Jobs) execute(parent context.Context, v Job) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Hour)
	defer cancel()
	j.mu.Lock()
	j.running[v.ID] = cancel
	j.mu.Unlock()
	defer func() { j.mu.Lock(); delete(j.running, v.ID); j.mu.Unlock() }()
	heartbeatDone := make(chan struct{})
	go func() {
		defer close(heartbeatDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				beat, done := context.WithTimeout(ctx, 5*time.Second)
				tag, err := j.Search.Store.DB.Exec(beat, `UPDATE similarity_jobs SET lease_until=now()+interval '1 minute' WHERE id=$1::uuid AND lease_token=$2::uuid AND status='running'`, v.ID, v.Token)
				done()
				if err != nil || tag.RowsAffected() != 1 {
					cancel()
					return
				}
			}
		}
	}()
	result, err := j.process(ctx, v)
	cancel()
	<-heartbeatDone
	final, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	if parent.Err() != nil {
		_, _ = j.Search.Store.DB.Exec(final, `UPDATE similarity_jobs SET status='queued',phase='Ожидание продолжения',lease_token=NULL,lease_until='-infinity' WHERE id=$1::uuid AND lease_token=$2::uuid AND status='running'`, v.ID, v.Token)
		return
	}
	if err != nil {
		tag, saveErr := j.Search.Store.DB.Exec(final, `UPDATE similarity_jobs SET status='failed',phase='Поиск не завершён',error='Не удалось подготовить историю или выполнить поиск. Повторите попытку.',finished_at=now(),lease_token=NULL WHERE id=$1::uuid AND lease_token=$2::uuid AND status='running'`, v.ID, v.Token)
		if saveErr != nil {
			log.Printf("similarity job %s state: %v", v.ID, saveErr)
		} else if tag.RowsAffected() == 1 {
			log.Printf("similarity job %s failed: %v", v.ID, err)
		}
		return
	}
	raw, _ := json.Marshal(result)
	_, err = j.Search.Store.DB.Exec(final, `UPDATE similarity_jobs SET status='completed',phase='Готово',progress=100,result=$3,finished_at=now(),lease_token=NULL WHERE id=$1::uuid AND lease_token=$2::uuid AND status='running'`, v.ID, v.Token, raw)
	if err != nil {
		log.Printf("similarity job %s result: %v", v.ID, err)
	} else {
		log.Printf("similarity job %s completed matches=%d", v.ID, len(result.Matches))
	}
}
func (j *Jobs) Run(ctx context.Context) {
	for ctx.Err() == nil {
		v, err := j.claim(ctx)
		if err != nil {
			log.Printf("similarity job claim: %v", err)
		}
		if err == nil && v != nil {
			j.execute(ctx, *v)
			continue
		}
		// Requests wake the worker immediately; this slow fallback recovers leases/restarts.
		select {
		case <-ctx.Done():
			return
		case <-j.wake:
		case <-time.After(30 * time.Second):
		}
	}
}
