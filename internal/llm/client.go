package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Request struct {
	Model       string
	Messages    []Message
	Temperature float64
	MaxTokens   int
}
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens"`
}
type Response struct {
	Content string
	Usage   Usage
}
type Client interface {
	Complete(context.Context, Request) (Response, error)
}
