package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/RobiNexy/book-forge/internal/llm"
)

// Client implements the LLM boundary using OpenAI's chat-completions endpoint.
type Client struct {
	APIKey, BaseURL string
	Headers         map[string]string
	HTTP            *http.Client
}
type response struct {
	Choices []struct {
		Message llm.Message `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
}

// Complete sends a context-cancelable request and returns the first assistant choice.
func (c *Client) Complete(ctx context.Context, r llm.Request) (llm.Response, error) {
	if c.APIKey == "" {
		return llm.Response{}, fmt.Errorf("OpenAI API key is empty")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	payload := map[string]any{
		"model": r.Model, "messages": r.Messages, "temperature": r.Temperature,
		"max_completion_tokens": r.MaxTokens,
	}
	for key, value := range r.Parameters {
		payload[key] = value
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return llm.Response{}, fmt.Errorf("encode OpenAI request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return llm.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range c.Headers {
		req.Header.Set(name, value)
	}
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := h.Do(req)
	if err != nil {
		return llm.Response{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return llm.Response{}, fmt.Errorf("read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llm.Response{}, fmt.Errorf("OpenAI HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out response
	if err := json.Unmarshal(body, &out); err != nil {
		return llm.Response{}, fmt.Errorf("decode OpenAI response: %w", err)
	}
	if len(out.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("OpenAI response has no choices")
	}
	usage := llm.Usage{
		PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens,
		CachedTokens: out.Usage.PromptTokensDetails.CachedTokens,
	}
	return llm.Response{Content: out.Choices[0].Message.Content, Usage: usage}, nil
}
