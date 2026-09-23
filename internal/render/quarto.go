package render

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Quarto runs the user's rendering tool without making rendering part of core generation.
func Quarto(ctx context.Context, project string) error {
	cmd := exec.CommandContext(ctx, "quarto", "render")
	cmd.Dir = project
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("quarto render: %w", err)
	}
	return nil
}
