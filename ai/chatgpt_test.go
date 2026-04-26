package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewChatGPTClientUsesDefaultHTTPTimeout(t *testing.T) {
	client := NewChatGPTClient("test-key")
	if client.HTTPClient == nil {
		t.Fatal("NewChatGPTClient() HTTPClient = nil, want default client")
	}
	if client.HTTPClient.Timeout != DefaultChatGPTHTTPTimeout {
		t.Fatalf("HTTPClient.Timeout = %s, want %s", client.HTTPClient.Timeout, DefaultChatGPTHTTPTimeout)
	}
}

func TestNewChatGPTClientFromEnvUsesDefaultHTTPTimeout(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	client := NewChatGPTClientFromEnv()
	if client.HTTPClient == nil {
		t.Fatal("NewChatGPTClientFromEnv() HTTPClient = nil, want default client")
	}
	if client.HTTPClient.Timeout != DefaultChatGPTHTTPTimeout {
		t.Fatalf("HTTPClient.Timeout = %s, want %s", client.HTTPClient.Timeout, DefaultChatGPTHTTPTimeout)
	}
}

func TestChatGPTClientHTTPClientFallbackUsesDefaultTimeout(t *testing.T) {
	client := &ChatGPTClient{}
	httpClient := client.httpClient()
	if httpClient == nil {
		t.Fatal("httpClient() = nil, want default client")
	}
	if httpClient.Timeout != DefaultChatGPTHTTPTimeout {
		t.Fatalf("httpClient().Timeout = %s, want %s", httpClient.Timeout, DefaultChatGPTHTTPTimeout)
	}
}

func TestChatGPTClientHTTPClientPreservesCustomClient(t *testing.T) {
	custom := &http.Client{Timeout: 5 * time.Second}
	client := &ChatGPTClient{HTTPClient: custom}
	if got := client.httpClient(); got != custom {
		t.Fatalf("httpClient() = %p, want custom %p", got, custom)
	}
}

func TestChatGPTClientGenerate(t *testing.T) {
	var gotAuth string
	var gotPath string
	var gotRequest chatGPTRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("Decode(request) error = %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "resp_test",
			"status": "completed",
			"output": [{
				"type": "message",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "translated markdown"}]
			}]
		}`))
	}))
	defer server.Close()

	client := NewChatGPTClient("test-key")
	client.BaseURL = server.URL + "/v1"
	client.Model = "test-model"
	client.MaxOutputTokens = 123

	got, err := client.Generate(context.Background(), "translate", "# Hello")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "translated markdown" {
		t.Fatalf("Generate() = %q, want translated markdown", got)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want bearer key", gotAuth)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path = %q, want /v1/responses", gotPath)
	}
	if gotRequest.Model != "test-model" {
		t.Fatalf("model = %q, want test-model", gotRequest.Model)
	}
	if gotRequest.Instructions != "translate" || gotRequest.Input != "# Hello" {
		t.Fatalf("request = %+v, missing instructions/input", gotRequest)
	}
	if gotRequest.MaxOutputTokens != 123 {
		t.Fatalf("max output tokens = %d, want 123", gotRequest.MaxOutputTokens)
	}
}

func TestChatGPTClientGenerateUsesTopLevelOutputText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed","output_text":"de"}`))
	}))
	defer server.Close()

	client := NewChatGPTClient("test-key")
	client.BaseURL = server.URL

	got, err := client.Generate(context.Background(), "detect", "Hallo")
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got != "de" {
		t.Fatalf("Generate() = %q, want de", got)
	}
}

func TestChatGPTClientGenerateRequiresAPIKey(t *testing.T) {
	client := NewChatGPTClient("")
	if _, err := client.Generate(context.Background(), "x", "y"); err == nil {
		t.Fatal("Generate() error = nil, want missing API key error")
	}
}

func TestChatGPTClientGenerateReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"bad key","code":"invalid_api_key"}}`))
	}))
	defer server.Close()

	client := NewChatGPTClient("test-key")
	client.BaseURL = server.URL

	_, err := client.Generate(context.Background(), "x", "y")
	if err == nil {
		t.Fatal("Generate() error = nil, want API error")
	}
	if !strings.Contains(err.Error(), "bad key") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("Generate() error = %v, want API message and status", err)
	}
}
