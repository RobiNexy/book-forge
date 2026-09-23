package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/llm"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	APIKey, BaseURL string
	HTTP            *http.Client
}
type request struct {
	Model       string        `json:"model"`
	Messages    []llm.Message `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}
type response struct {
	Choices []struct {
		Message llm.Message `json:"message"`
	} `json:"choices"`
	Usage llm.Usage `json:"usage"`
}

func (c *Client) Complete(ctx context.Context, r llm.Request) (llm.Response, error) {
	if c.APIKey == "" {
		return llm.Response{}, fmt.Errorf("OpenAI API key is empty")
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	b, _ := json.Marshal(request{r.Model, r.Messages, r.Temperature, r.MaxTokens})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return llm.Response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 5 * time.Minute}
	}
	resp, err := h.Do(req)
	if err != nil {
		return llm.Response{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return llm.Response{}, fmt.Errorf("OpenAI HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var out response
	if err := json.Unmarshal(body, &out); err != nil {
		return llm.Response{}, err
	}
	if len(out.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("OpenAI response has no choices")
	}
	return llm.Response{Content: out.Choices[0].Message.Content, Usage: out.Usage}, nil
}
