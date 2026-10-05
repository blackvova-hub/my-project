package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestOwnedACKRejectsConsumerLossIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	stream, group := "codex:test:owned-ack:"+suffix, "codex-test-owned-ack-"+suffix
	defer client.Del(ctx, stream)
	message := addAndReadPending(t, ctx, client, stream, group, "consumer-a", map[string]any{"json": `{"ok":true}`})
	claimed, err := client.XClaimJustID(ctx, &redis.XClaimArgs{Stream: stream, Group: group, Consumer: "consumer-b", MinIdle: 0, Messages: []string{message.ID}}).Result()
	if err != nil || len(claimed) != 1 {
		t.Fatalf("owner transfer ids=%+v err=%v", claimed, err)
	}
	acked, err := ackOwnedStreamMessage(ctx, client, stream, group, "consumer-a", message.ID)
	if acked || !errors.Is(err, errStreamMessageNotOwner) {
		t.Fatalf("stale owner ACK result=%v err=%v", acked, err)
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 1 {
		t.Fatalf("owner loss removed PEL entry: pending=%+v err=%v", pending, err)
	}
}
