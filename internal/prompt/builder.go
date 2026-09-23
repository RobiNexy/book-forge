package prompt

import (
	"encoding/json"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/llm"
)

const system = "You are BookForge, a careful book chapter writer. Follow the output protocol exactly: write chapter prose between <<<CHAPTER>>> and <<<HOOKS>>>, then strict JSON with discharged, rewritten, and added arrays, and finish with <<<END>>>. Never invent hook IDs."

// Build keeps the stable instruction and outline prefix before per-chapter state.
func Build(outline string, pool domain.HookPool, n int) ([]llm.Message, error) {
	b, err := json.Marshal(pool)
	if err != nil {
		return nil, err
	}
	return []llm.Message{{Role: "system", Content: system}, {Role: "system", Content: outline}, {Role: "user", Content: string(b)}, {Role: "user", Content: fmt.Sprintf("Generate chapter %d. Preserve continuity and report hook operations.", n)}}, nil
}
