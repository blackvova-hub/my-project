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

func TestStreamDLQName(t *testing.T) {
	if got, want := streamDLQName("candles:v3:bybit:perpetual"), "candles:v3:bybit:perpetual:dlq"; got != want {
		t.Fatalf("DLQ name=%q want=%q", got, want)
	}
}

func TestMovePendingStreamMessageToDLQAtomicIntegration(t *testing.T) {
	address := strings.TrimSpace(os.Getenv("TEST_REDIS_ADDR"))
	if address == "" {
		t.Skip("TEST_REDIS_ADDR is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	suffix := strconv.FormatInt(time.Now().UnixNano(), 36)
	stream, group, consumer := "codex:test:dlq:"+suffix, "codex-test-dlq-"+suffix, "test-consumer"
	defer client.Del(ctx, stream, streamDLQName(stream))
	messageID, err := client.XAdd(ctx, &redis.XAddArgs{Stream: stream, Values: map[string]any{"json": `{"value":1}`}}).Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.XGroupCreate(ctx, stream, group, "0-0").Err(); err != nil {
		t.Fatal(err)
	}
	read, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: group, Consumer: consumer, Streams: []string{stream, ">"}, Count: 1}).Result()
	if err != nil || len(read) != 1 || len(read[0].Messages) != 1 {
		t.Fatalf("read pending message: streams=%+v err=%v", read, err)
	}
	message := read[0].Messages[0]
	if message.ID != messageID {
		t.Fatalf("message id=%s want=%s", message.ID, messageID)
	}
	deliveries, err := streamDeliveryCount(ctx, client, stream, group, message.ID)
	if err != nil || deliveries != 1 {
		t.Fatalf("delivery count=%d err=%v", deliveries, err)
	}
	dlqID, err := movePendingStreamMessageToDLQ(ctx, client, stream, group, message, streamFailurePermanent, errors.New("invalid payload"), deliveries)
	if err != nil || dlqID == "" {
		t.Fatalf("move to DLQ id=%q err=%v", dlqID, err)
	}
	pending, err := client.XPending(ctx, stream, group).Result()
	if err != nil || pending.Count != 0 {
		t.Fatalf("pending after DLQ=%+v err=%v", pending, err)
	}
	dlq, err := client.XRange(ctx, streamDLQName(stream), "-", "+").Result()
	if err != nil || len(dlq) != 1 {
		t.Fatalf("DLQ entries=%+v err=%v", dlq, err)
	}
	if dlq[0].Values["source_id"] != message.ID || dlq[0].Values["failure_class"] != string(streamFailurePermanent) || dlq[0].Values["delivery_count"] != "1" {
		t.Fatalf("DLQ metadata=%+v", dlq[0].Values)
	}

	// A non-pending source must not create another audit record.
	if _, err := movePendingStreamMessageToDLQ(ctx, client, stream, group, message, streamFailurePermanent, errors.New("again"), deliveries); err == nil {
		t.Fatal("non-pending source was moved twice")
	}
	if length := client.XLen(ctx, streamDLQName(stream)).Val(); length != 1 {
		t.Fatalf("DLQ length=%d want=1", length)
	}
}
