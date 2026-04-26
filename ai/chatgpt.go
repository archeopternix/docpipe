package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultChatGPTBaseURL = "https://api.openai.com/v1"
	DefaultChatGPTModel   = "gpt-5.4-mini"
)

// ChatGPTClient implements Client using OpenAI's Responses API.
type ChatGPTClient struct {
	APIKey          string
	Model           string
	BaseURL         string
	HTTPClient      *http.Client
	MaxOutputTokens int
}

// NewChatGPTClient creates a ChatGPT-backed AI client.
func NewChatGPTClient(apiKey string) *ChatGPTClient {
	return &ChatGPTClient{
		APIKey:  strings.TrimSpace(apiKey),
		Model:   DefaultChatGPTModel,
		BaseURL: DefaultChatGPTBaseURL,
	}
}

// NewChatGPTClientFromEnv creates a ChatGPT-backed AI client from environment variables.
//
// Supported variables:
//   - OPENAI_API_KEY: required API key
//   - OPENAI_MODEL: optional model override
//   - OPENAI_BASE_URL: optional API base URL override
func NewChatGPTClientFromEnv() *ChatGPTClient {
	client := NewChatGPTClient(os.Getenv("OPENAI_API_KEY"))
	if model := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); model != "" {
		client.Model = model
	}
	if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
		client.BaseURL = baseURL
	}
	return client
}

func (c *ChatGPTClient) Generate(ctx context.Context, instructions, input string) (string, error) {
	if c == nil {
		return "", fmt.Errorf("ai: chatgpt client is nil")
	}
	apiKey := strings.TrimSpace(c.APIKey)
	if apiKey == "" {
		return "", fmt.Errorf("ai: OPENAI_API_KEY is required")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		return "", fmt.Errorf("ai: OpenAI model is required")
	}

	endpoint, err := chatGPTEndpoint(c.BaseURL)
	if err != nil {
		return "", err
	}

	reqBody := chatGPTRequest{
		Model:        model,
		Instructions: instructions,
		Input:        input,
	}
	if c.MaxOutputTokens > 0 {
		reqBody.MaxOutputTokens = c.MaxOutputTokens
	}

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(reqBody); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", chatGPTAPIError(resp.StatusCode, respBody)
	}

	var decoded chatGPTResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return "", err
	}
	if decoded.Error.Message != "" {
		return "", fmt.Errorf("openai: %s", decoded.Error.Message)
	}
	text := decoded.text()
	if strings.TrimSpace(text) == "" {
		if decoded.refusal() != "" {
			return "", fmt.Errorf("openai: model refused request: %s", decoded.refusal())
		}
		if decoded.Status != "" && decoded.Status != "completed" {
			return "", fmt.Errorf("openai: response status %q did not include text", decoded.Status)
		}
		return "", fmt.Errorf("openai: response did not include text")
	}
	return text, nil
}

type chatGPTRequest struct {
	Model           string `json:"model"`
	Instructions    string `json:"instructions,omitempty"`
	Input           string `json:"input"`
	MaxOutputTokens int    `json:"max_output_tokens,omitempty"`
}

type chatGPTResponse struct {
	ID         string              `json:"id"`
	Status     string              `json:"status"`
	OutputText string              `json:"output_text"`
	Output     []chatGPTOutputItem `json:"output"`
	Error      struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type chatGPTOutputItem struct {
	Type    string               `json:"type"`
	Role    string               `json:"role"`
	Content []chatGPTContentItem `json:"content"`
}

type chatGPTContentItem struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Refusal string `json:"refusal"`
}

func (r chatGPTResponse) text() string {
	if r.OutputText != "" {
		return r.OutputText
	}
	var parts []string
	for _, out := range r.Output {
		if out.Type != "message" {
			continue
		}
		for _, content := range out.Content {
			if content.Type == "output_text" && content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.Join(parts, "")
}

func (r chatGPTResponse) refusal() string {
	for _, out := range r.Output {
		if out.Type != "message" {
			continue
		}
		for _, content := range out.Content {
			if content.Type == "refusal" && content.Refusal != "" {
				return content.Refusal
			}
		}
	}
	return ""
}

func chatGPTEndpoint(baseURL string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = DefaultChatGPTBaseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("ai: invalid OpenAI base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("ai: invalid OpenAI base URL %q", baseURL)
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/responses"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func chatGPTAPIError(statusCode int, body []byte) error {
	var decoded struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err == nil && decoded.Error.Message != "" {
		if decoded.Error.Code != "" {
			return fmt.Errorf("openai: %s (%s, HTTP %d)", decoded.Error.Message, decoded.Error.Code, statusCode)
		}
		return fmt.Errorf("openai: %s (HTTP %d)", decoded.Error.Message, statusCode)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return fmt.Errorf("openai: HTTP %d", statusCode)
	}
	return fmt.Errorf("openai: HTTP %d: %s", statusCode, text)
}
