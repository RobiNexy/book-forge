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

// Client implements the LLM boundary using OpenAI-compatible chat-completions or Responses APIs.
type Client struct {
	APIKey, BaseURL string
	APIFormat       string
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

type responsesResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		InputDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"input_tokens_details"`
	} `json:"usage"`
}

// Complete sends a context-cancelable request and returns the first assistant choice.
func (c *Client) Complete(ctx context.Context, r llm.Request) (llm.Response, error) {
	if c.APIKey == "" {
		return llm.Response{}, llm.PermanentError{Err: fmt.Errorf("OpenAI API key is empty")}
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	format := c.APIFormat
	if format == "" {
		format = "chat_completions"
	}
	if format != "chat_completions" && format != "responses" {
		return llm.Response{}, fmt.Errorf("unsupported OpenAI API format %q", format)
	}
	path := "/chat/completions"
	payload := map[string]any{"model": r.Model, "temperature": r.Temperature}
	if format == "responses" {
		path = "/responses"
		payload["input"] = r.Messages
		payload["max_output_tokens"] = r.MaxTokens
	} else {
		payload["messages"] = r.Messages
		payload["max_completion_tokens"] = r.MaxTokens
	}
	for key, value := range r.Parameters {
		payload[key] = value
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return llm.Response{}, fmt.Errorf("encode OpenAI request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(b))
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
		err := fmt.Errorf("OpenAI HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusTooManyRequests {
			return llm.Response{}, llm.PermanentError{Err: err}
		}
		return llm.Response{}, err
	}
	if format == "responses" {
		var out responsesResponse
		if err := json.Unmarshal(body, &out); err != nil {
			return llm.Response{}, fmt.Errorf("decode OpenAI Responses response: %w", err)
		}
		content := out.OutputText
		if content == "" {
			var parts []string
			for _, item := range out.Output {
				for _, part := range item.Content {
					if part.Type == "output_text" && part.Text != "" {
						parts = append(parts, part.Text)
					}
				}
			}
			content = strings.Join(parts, "")
		}
		if content == "" {
			return llm.Response{}, fmt.Errorf("OpenAI Responses response has no output text")
		}
		usage := llm.Usage{
			PromptTokens: out.Usage.InputTokens, CompletionTokens: out.Usage.OutputTokens,
			CachedTokens: out.Usage.InputDetails.CachedTokens,
		}
		return llm.Response{Content: content, Usage: usage}, nil
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
