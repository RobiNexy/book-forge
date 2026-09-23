package domain

type HookPool struct {
	N       int    `json:"n"`
	Hooks   []Hook `json:"hooks"`
	Version string `json:"version"`
}
