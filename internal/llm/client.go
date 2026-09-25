package llm

import "context"

// Message is one ordered chat message. Ordering is part of the prompt-caching contract.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Request contains one complete model operation and its per-run generation parameters.
type Request struct {
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
	Parameters  map[string]any
}

// Usage contains provider-reported token accounting, including cached input tokens when known.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}

// Response is the generated assistant text and provider usage metadata.
type Response struct {
	Content string
	Usage   Usage
}

// Client is the infrastructure boundary consumed by the chapter orchestrator.
type Client interface {
	Complete(context.Context, Request) (Response, error)
}
