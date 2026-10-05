package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type pgCompactBootstrapOwner struct {
	mu       sync.Mutex
	conn     *pgxpool.Conn
	identity string
	pid      int32
	closed   bool
}

func acquireCompactBootstrapOwner(ctx context.Context, db *pgxpool.Pool, group string) (*pgCompactBootstrapOwner, bool, error) {
	group = strings.TrimSpace(group)
	if db == nil || group == "" {
		return nil, false, errors.New("invalid compact bootstrap owner configuration")
	}
	conn, err := db.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	identity := "compact-v3:" + group
	var pid int32
	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_backend_pid(), pg_try_advisory_lock(hashtextextended($1, 0))`, identity).Scan(&pid, &acquired); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !acquired {
		conn.Release()
		return nil, false, nil
	}
	return &pgCompactBootstrapOwner{conn: conn, identity: identity, pid: pid}, true, nil
}

func (o *pgCompactBootstrapOwner) AssertOwned(ctx context.Context) error {
	if o == nil {
		return errCompactBootstrapOwnerRequired
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || o.conn == nil {
		return errors.New("compact bootstrap owner is closed")
	}
	var pid int32
	if err := o.conn.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		return fmt.Errorf("verify compact bootstrap owner connection: %w", err)
	}
	if pid != o.pid {
		return fmt.Errorf("compact bootstrap owner backend changed: got=%d want=%d", pid, o.pid)
	}
	return nil
}

func (o *pgCompactBootstrapOwner) Close(ctx context.Context) error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil
	}
	o.closed = true
	if o.conn == nil {
		return nil
	}
	var unlocked bool
	err := o.conn.QueryRow(ctx, `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, o.identity).Scan(&unlocked)
	o.conn.Release()
	o.conn = nil
	if err != nil {
		return err
	}
	if !unlocked {
		return errors.New("compact bootstrap advisory lock was not held")
	}
	return nil
}
