package domain

type HookType string

const (
	HookFact     HookType = "fact"
	HookTension  HookType = "tension"
	HookCallback HookType = "callback"
)

type Hook struct {
	ID      string   `json:"id"`
	Src     int      `json:"src"`
	Type    HookType `json:"type"`
	Content string   `json:"content"`
}
