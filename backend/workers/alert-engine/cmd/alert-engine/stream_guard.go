package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var errStreamMessageBusy = errors.New("stream message is already in flight")

var heartbeatStreamBatchScript = redis.NewScript(`
local result = {}
for index = 3, #ARGV do
  local message_id = ARGV[index]
  local pending = redis.call('XPENDING', KEYS[1], ARGV[1], message_id, message_id, 1)
  if #pending == 0 then
    table.insert(result, message_id)
    table.insert(result, 0)
    table.insert(result, 0)
  elseif pending[1][2] ~= ARGV[2] then
    table.insert(result, message_id)
    table.insert(result, -1)
    table.insert(result, pending[1][4])
  else
    local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, message_id, 'JUSTID')
    table.insert(result, message_id)
    if #claimed == 1 then
      table.insert(result, 1)
    else
      table.insert(result, -2)
    end
    table.insert(result, pending[1][4])
  end
end
return result
`)

var localStreamInflight = struct {
	sync.Mutex
	entries map[string]struct{}
}{entries: make(map[string]struct{})}

type streamBatchGuard struct {
	client       *redis.Client
	stream       string
	group        string
	consumer     string
	messageIDs   []string
	registryKeys []string
	interval     time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	closeOnce    sync.Once

	mu         sync.RWMutex
	owned      map[string]bool
	deliveries map[string]int64
	contexts   map[string]context.Context
	cancels    map[string]context.CancelFunc
}

// startStreamBatchGuard registers the entire XREADGROUP/XAUTOCLAIM reply as
// in-flight and periodically refreshes each still-pending entry with
// XCLAIM ... JUSTID. JUSTID resets idle time without incrementing RetryCount,
// so an active slow batch cannot be reclaimed concurrently and cannot be
// falsely exhausted merely because processing takes longer than MinIdle.
func startStreamBatchGuard(parent context.Context, client *redis.Client, stream, group, consumer string, messageIDs []string, minIdle time.Duration) (*streamBatchGuard, error) {
	if client == nil || parent == nil || stream == "" || group == "" || consumer == "" || len(messageIDs) == 0 {
		return nil, errors.New("invalid stream batch guard identity")
	}
	if minIdle <= 0 {
		minIdle = 15 * time.Second
	}
	interval := minIdle / 3
	if interval < 25*time.Millisecond {
		interval = 25 * time.Millisecond
	}
	if interval > 5*time.Second {
		interval = 5 * time.Second
	}
	unique := make([]string, 0, len(messageIDs))
	seen := make(map[string]struct{}, len(messageIDs))
	registryKeys := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		if id == "" {
			return nil, errors.New("stream batch guard contains an empty message id")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
		registryKeys = append(registryKeys, fmt.Sprintf("%p\x00%s\x00%s\x00%s", client, stream, group, id))
	}
	localStreamInflight.Lock()
	for _, key := range registryKeys {
		if _, exists := localStreamInflight.entries[key]; exists {
			localStreamInflight.Unlock()
			return nil, errStreamMessageBusy
		}
	}
	for _, key := range registryKeys {
		localStreamInflight.entries[key] = struct{}{}
	}
	localStreamInflight.Unlock()

	guardCtx, cancel := context.WithCancel(parent)
	guard := &streamBatchGuard{
		client: client, stream: stream, group: group, consumer: consumer,
		messageIDs: unique, registryKeys: registryKeys, interval: interval,
		ctx: guardCtx, cancel: cancel, done: make(chan struct{}),
		owned: make(map[string]bool, len(unique)), deliveries: make(map[string]int64, len(unique)),
		contexts: make(map[string]context.Context, len(unique)), cancels: make(map[string]context.CancelFunc, len(unique)),
	}
	for _, id := range unique {
		messageCtx, messageCancel := context.WithCancel(guardCtx)
		guard.owned[id] = true
		guard.contexts[id] = messageCtx
		guard.cancels[id] = messageCancel
	}
	go guard.heartbeatLoop()
	return guard, nil
}

func (g *streamBatchGuard) MessageContext(messageID string) context.Context {
	if g == nil {
		return context.Background()
	}
	g.mu.RLock()
	ctx := g.contexts[messageID]
	g.mu.RUnlock()
	if ctx == nil {
		return g.ctx
	}
	return ctx
}

func (g *streamBatchGuard) Owns(messageID string) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	owned := g.owned[messageID]
	g.mu.RUnlock()
	return owned
}

func (g *streamBatchGuard) DeliveryCount(messageID string) int64 {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	deliveries := g.deliveries[messageID]
	g.mu.RUnlock()
	return deliveries
}

func (g *streamBatchGuard) Complete(messageID string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.owned[messageID] {
		g.owned[messageID] = false
		if cancel := g.cancels[messageID]; cancel != nil {
			cancel()
		}
	}
	g.mu.Unlock()
}

func (g *streamBatchGuard) Close() {
	if g == nil {
		return
	}
	g.closeOnce.Do(func() {
		g.cancel()
		<-g.done
		localStreamInflight.Lock()
		for _, key := range g.registryKeys {
			delete(localStreamInflight.entries, key)
		}
		localStreamInflight.Unlock()
	})
}

func (g *streamBatchGuard) heartbeatLoop() {
	defer close(g.done)
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-g.ctx.Done():
			g.cancelAll()
			return
		case <-ticker.C:
			if !g.heartbeat() && !g.anyOwned() {
				return
			}
		}
	}
}

func (g *streamBatchGuard) heartbeat() bool {
	ids := g.ownedIDs()
	if len(ids) == 0 {
		return false
	}
	args := make([]any, 0, len(ids)+2)
	args = append(args, g.group, g.consumer)
	for _, id := range ids {
		args = append(args, id)
	}
	result, err := heartbeatStreamBatchScript.Run(g.ctx, g.client, []string{g.stream}, args...).Slice()
	if err != nil {
		return true
	}
	if len(result)%3 != 0 {
		return true
	}
	for index := 0; index < len(result); index += 3 {
		id := fmt.Sprint(result[index])
		status, statusErr := streamScriptInt64(result[index+1])
		deliveries, deliveriesErr := streamScriptInt64(result[index+2])
		if statusErr != nil || deliveriesErr != nil {
			continue
		}
		g.mu.Lock()
		g.deliveries[id] = deliveries
		if status <= 0 && g.owned[id] {
			g.owned[id] = false
			if cancel := g.cancels[id]; cancel != nil {
				cancel()
			}
		}
		g.mu.Unlock()
	}
	return true
}

func (g *streamBatchGuard) ownedIDs() []string {
	g.mu.RLock()
	ids := make([]string, 0, len(g.messageIDs))
	for _, id := range g.messageIDs {
		if g.owned[id] {
			ids = append(ids, id)
		}
	}
	g.mu.RUnlock()
	return ids
}

func (g *streamBatchGuard) anyOwned() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	for _, owned := range g.owned {
		if owned {
			return true
		}
	}
	return false
}

func (g *streamBatchGuard) cancelAll() {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id, cancel := range g.cancels {
		if cancel != nil {
			cancel()
		}
		g.owned[id] = false
	}
}

func streamScriptInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case int64:
		return typed, nil
	case string:
		return strconv.ParseInt(typed, 10, 64)
	case []byte:
		return strconv.ParseInt(string(typed), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis script integer %T", value)
	}
}
