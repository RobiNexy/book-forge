package render

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Quarto runs the user's rendering tool without making rendering part of core generation.
func Quarto(ctx context.Context, project string) error {
	return QuartoCommand(ctx, project, "quarto")
}

// QuartoCommand invokes the configured Quarto executable from the user's project directory.
func QuartoCommand(ctx context.Context, project, executable string) error {
	cmd := exec.CommandContext(ctx, executable, "render")
	cmd.Dir = project
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("quarto render: %w", err)
	}
	return nil
}
