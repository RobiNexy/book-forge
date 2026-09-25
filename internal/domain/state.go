package domain

// ProjectState stores only the latest opaque hook text and whether the model ended the book.
type ProjectState struct {
	CurrentHooks string `json:"current_hooks"`
	Finished     bool   `json:"finished"`
}
