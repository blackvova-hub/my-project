package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	dlqSchemaVersion     = "1"
	replayReferenceField = "_dlq_replay_ref"
	replaySourceIDField  = "_dlq_replay_source_id"
	replayTimestampField = "_dlq_replayed_at_unix_ms"
)

type replayOptions struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	DLQStream     string
	DLQID         string
	Execute       bool
	Timeout       time.Duration
}

type dlqAuditRecord struct {
	DLQStream    string
	DLQID        string
	SourceStream string
	SourceID     string
	RawValues    []byte
	RawSize      int64
	RawSHA256    string
	RawTruncated bool
}

type replayPlan struct {
	Mode         string `json:"mode"`
	DLQStream    string `json:"dlq_stream"`
	DLQID        string `json:"dlq_id"`
	SourceStream string `json:"source_stream"`
	SourceID     string `json:"source_id"`
	PayloadFrom  string `json:"payload_from"`
	NewID        string `json:"new_id,omitempty"`
}

func main() {
	if err := run(context.Background(), os.Stdout, os.Stderr, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dlq-replay: %v\n", err)
		os.Exit(1)
	}
}

func run(parent context.Context, stdout, _ io.Writer, args []string) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, options.Timeout)
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: options.RedisAddr, Password: options.RedisPassword, DB: options.RedisDB})
	defer client.Close()

	dlqMessages, err := client.XRangeN(ctx, options.DLQStream, options.DLQID, options.DLQID, 1).Result()
	if err != nil {
		return fmt.Errorf("read DLQ audit record: %w", err)
	}
	if len(dlqMessages) != 1 || dlqMessages[0].ID != options.DLQID {
		return errors.New("DLQ audit record was not found")
	}
	record, err := parseDLQAuditRecord(options.DLQStream, dlqMessages[0])
	if err != nil {
		return err
	}
	values, payloadFrom, err := resolveReplayValues(ctx, client, record)
	if err != nil {
		return err
	}
	if _, exists := values[replayReferenceField]; exists {
		return fmt.Errorf("source payload already contains reserved field %q", replayReferenceField)
	}
	if _, exists := values[replaySourceIDField]; exists {
		return fmt.Errorf("source payload already contains reserved field %q", replaySourceIDField)
	}
	if _, exists := values[replayTimestampField]; exists {
		return fmt.Errorf("source payload already contains reserved field %q", replayTimestampField)
	}
	plan := replayPlan{Mode: "dry-run", DLQStream: record.DLQStream, DLQID: record.DLQID, SourceStream: record.SourceStream, SourceID: record.SourceID, PayloadFrom: payloadFrom}
	if options.Execute {
		values[replayReferenceField] = record.DLQStream + "@" + record.DLQID
		values[replaySourceIDField] = record.SourceID
		values[replayTimestampField] = strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
		newID, addErr := client.XAdd(ctx, &redis.XAddArgs{Stream: record.SourceStream, Values: values}).Result()
		if addErr != nil {
			return fmt.Errorf("append replay to source stream: %w", addErr)
		}
		plan.Mode = "execute"
		plan.NewID = newID
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(encoded))
	return err
}

func parseOptions(args []string) (replayOptions, error) {
	options := replayOptions{
		RedisAddr:     strings.TrimSpace(os.Getenv("REDIS_ADDR")),
		RedisPassword: os.Getenv("REDIS_PASSWORD"),
		Timeout:       10 * time.Second,
	}
	if options.RedisAddr == "" {
		options.RedisAddr = "127.0.0.1:6379"
	}
	if rawDB := strings.TrimSpace(os.Getenv("REDIS_DB")); rawDB != "" {
		if parsed, err := strconv.Atoi(rawDB); err == nil {
			options.RedisDB = parsed
		}
	}
	flags := flag.NewFlagSet("dlq-replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.RedisAddr, "redis-addr", options.RedisAddr, "Redis host:port")
	flags.StringVar(&options.RedisPassword, "redis-password", options.RedisPassword, "Redis password")
	flags.IntVar(&options.RedisDB, "redis-db", options.RedisDB, "Redis database")
	flags.StringVar(&options.DLQStream, "dlq-stream", "", "DLQ stream name")
	flags.StringVar(&options.DLQID, "id", "", "DLQ audit record ID")
	flags.BoolVar(&options.Execute, "execute", false, "append replay to the source stream")
	flags.DurationVar(&options.Timeout, "timeout", options.Timeout, "operation timeout")
	if err := flags.Parse(args); err != nil {
		return replayOptions{}, err
	}
	if flags.NArg() != 0 {
		return replayOptions{}, errors.New("unexpected positional arguments")
	}
	options.RedisAddr = strings.TrimSpace(options.RedisAddr)
	options.DLQStream = strings.TrimSpace(options.DLQStream)
	options.DLQID = strings.TrimSpace(options.DLQID)
	if options.RedisAddr == "" || options.DLQStream == "" || options.DLQID == "" || options.Timeout <= 0 {
		return replayOptions{}, errors.New("--dlq-stream and --id are required; Redis address and timeout must be valid")
	}
	return options, nil
}

func parseDLQAuditRecord(dlqStream string, message redis.XMessage) (dlqAuditRecord, error) {
	field := func(name string) (string, error) {
		value, exists := message.Values[name]
		if !exists {
			return "", fmt.Errorf("DLQ metadata field %q is missing", name)
		}
		text := strings.TrimSpace(fmt.Sprint(value))
		if text == "" {
			return "", fmt.Errorf("DLQ metadata field %q is empty", name)
		}
		return text, nil
	}
	schema, err := field("schema_version")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	if schema != dlqSchemaVersion {
		return dlqAuditRecord{}, fmt.Errorf("unsupported DLQ schema version %q", schema)
	}
	sourceStream, err := field("source_stream")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	if dlqStream != sourceStream+":dlq" {
		return dlqAuditRecord{}, errors.New("DLQ stream does not match source_stream metadata")
	}
	sourceID, err := field("source_id")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	rawSizeText, err := field("raw_size")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	rawSize, err := strconv.ParseInt(rawSizeText, 10, 64)
	if err != nil || rawSize < 0 {
		return dlqAuditRecord{}, errors.New("invalid raw_size metadata")
	}
	rawSHA, err := field("raw_sha256")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	decodedSHA, err := hex.DecodeString(rawSHA)
	if err != nil || len(decodedSHA) != sha256.Size {
		return dlqAuditRecord{}, errors.New("invalid raw_sha256 metadata")
	}
	truncatedText, err := field("raw_truncated")
	if err != nil {
		return dlqAuditRecord{}, err
	}
	if truncatedText != "0" && truncatedText != "1" {
		return dlqAuditRecord{}, errors.New("invalid raw_truncated metadata")
	}
	rawValue, exists := message.Values["raw_values"]
	if !exists {
		return dlqAuditRecord{}, errors.New("DLQ metadata field \"raw_values\" is missing")
	}
	raw := []byte(fmt.Sprint(rawValue))
	if int64(len(raw)) > rawSize || (truncatedText == "0" && int64(len(raw)) != rawSize) {
		return dlqAuditRecord{}, errors.New("raw_values length contradicts metadata")
	}
	return dlqAuditRecord{DLQStream: dlqStream, DLQID: message.ID, SourceStream: sourceStream, SourceID: sourceID, RawValues: raw, RawSize: rawSize, RawSHA256: strings.ToLower(rawSHA), RawTruncated: truncatedText == "1"}, nil
}

func resolveReplayValues(ctx context.Context, client *redis.Client, record dlqAuditRecord) (map[string]any, string, error) {
	if !record.RawTruncated {
		if err := verifyRaw(record.RawValues, record.RawSize, record.RawSHA256); err != nil {
			return nil, "", err
		}
		values, err := decodeRawValues(record.RawValues)
		return values, "dlq_audit", err
	}
	sourceMessages, err := client.XRangeN(ctx, record.SourceStream, record.SourceID, record.SourceID, 1).Result()
	if err != nil {
		return nil, "", fmt.Errorf("read original truncated source payload: %w", err)
	}
	if len(sourceMessages) != 1 || sourceMessages[0].ID != record.SourceID {
		return nil, "", errors.New("raw_values is truncated and the original source ID is no longer retained")
	}
	raw, err := json.Marshal(sourceMessages[0].Values)
	if err != nil {
		return nil, "", fmt.Errorf("encode retained source payload: %w", err)
	}
	if err := verifyRaw(raw, record.RawSize, record.RawSHA256); err != nil {
		return nil, "", fmt.Errorf("retained source payload does not match DLQ audit: %w", err)
	}
	if !strings.HasPrefix(string(raw), string(record.RawValues)) {
		return nil, "", errors.New("retained source payload does not match the archived prefix")
	}
	values := make(map[string]any, len(sourceMessages[0].Values)+3)
	for key, value := range sourceMessages[0].Values {
		values[key] = value
	}
	return values, "source_id", nil
}

func verifyRaw(raw []byte, expectedSize int64, expectedSHA string) error {
	if int64(len(raw)) != expectedSize {
		return errors.New("raw payload size mismatch")
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != strings.ToLower(expectedSHA) {
		return errors.New("raw payload SHA-256 mismatch")
	}
	return nil
}

func decodeRawValues(raw []byte) (map[string]any, error) {
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("decode archived source values: %w", err)
	}
	if len(values) == 0 {
		return nil, errors.New("archived source values are empty")
	}
	result := make(map[string]any, len(values)+3)
	for key, value := range values {
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("archived source values contain an empty field name")
		}
		result[key] = value
	}
	return result, nil
}
