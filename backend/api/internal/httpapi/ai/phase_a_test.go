package aiapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseInflightDeletesZeroEntry(t *testing.T) {
	a := &API{inflight: map[int64]int{7: 1, 8: 2}}
	a.releaseInflight(7)
	a.releaseInflight(8)

	if _, ok := a.inflight[7]; ok {
		t.Fatal("zero inflight entry was retained")
	}
	if got := a.inflight[8]; got != 1 {
		t.Fatalf("inflight[8]=%d want=1", got)
	}
}

func TestReadLimitedResponseRejectsOversizeBody(t *testing.T) {
	got, err := readLimitedResponse(bytes.NewBufferString("12345"), 5)
	if err != nil || string(got) != "12345" {
		t.Fatalf("exact-limit response got=%q err=%v", got, err)
	}
	if _, err := readLimitedResponse(bytes.NewBufferString("123456"), 5); !errors.Is(err, errUpstreamResponseTooLarge) {
		t.Fatalf("oversize response err=%v", err)
	}
}

func TestNewDefaultsToGPT54Mini(t *testing.T) {
	a := New(nil, Config{})
	if got := a.cfg.OpenAIModel; got != "gpt-5.4-mini" {
		t.Fatalf("OpenAIModel=%q want gpt-5.4-mini", got)
	}
	if got := a.cfg.OpenAIBaseURL; got != "https://api.openai.com/v1" {
		t.Fatalf("OpenAIBaseURL=%q want official API base URL", got)
	}
}

func TestCallOpenAIUsesResponsesAPI(t *testing.T) {
	var got openAIResponseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path=%q want /v1/responses", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization=%q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-5.4-mini","output":[{"type":"message","content":[{"type":"output_text","text":"Готово"}]}]}`))
	}))
	defer server.Close()

	a := New(nil, Config{OpenAIAPIKey: "test-key", OpenAIBaseURL: server.URL + "/v1"})
	payload := openAIResponseRequest{
		Model:            a.cfg.OpenAIModel,
		Instructions:     "Короткая инструкция",
		Input:            []openAIInputMessage{{Role: "user", Content: "test"}},
		MaxOutputTokens:  maxAIOutputTokens,
		Reasoning:        openAIReasoningConfig{Effort: "low"},
		Text:             openAITextConfig{Verbosity: "low"},
		SafetyIdentifier: "safe-user-id",
		Store:            false,
	}
	body, status, err := a.callOpenAI(context.Background(), a.cfg.OpenAIBaseURL, payload)
	if err != nil {
		t.Fatalf("callOpenAI: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("status=%d", status)
	}
	if got.Model != "gpt-5.4-mini" || got.Instructions != "Короткая инструкция" || got.MaxOutputTokens != maxAIOutputTokens {
		t.Fatalf("unexpected request: %+v", got)
	}
	if len(got.Input) != 1 || got.Input[0].Role != "user" || got.Input[0].Content != "test" {
		t.Fatalf("unexpected role-preserving input: %+v", got.Input)
	}
	if got.Reasoning.Effort != "low" || got.Text.Verbosity != "low" || got.Store {
		t.Fatalf("unexpected response controls: %+v", got)
	}
	if got.SafetyIdentifier != "safe-user-id" {
		t.Fatalf("safety_identifier=%q", got.SafetyIdentifier)
	}

	var response openAIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if text := extractOpenAIText(response); text != "Готово" {
		t.Fatalf("text=%q want Готово", text)
	}
}

func TestExtractOpenAITextPrefersTopLevelOutputText(t *testing.T) {
	response := openAIResponse{OutputText: "  Короткий ответ  "}
	if got := extractOpenAIText(response); got != "Короткий ответ" {
		t.Fatalf("text=%q", got)
	}
}

func TestEstimateTokensAccountsForCyrillicUTF8(t *testing.T) {
	if got := estimateTokens("тест"); got != 2 {
		t.Fatalf("estimateTokens(тест)=%d want 2", got)
	}
}

func TestSafetyIdentifierIsStableAndDoesNotExposeUserID(t *testing.T) {
	first := safetyIdentifier("user@example.com")
	second := safetyIdentifier("user@example.com")
	if first == "" || first != second || strings.Contains(first, "user") || len(first) > 64 {
		t.Fatalf("unsafe safety identifier: %q", first)
	}
}
