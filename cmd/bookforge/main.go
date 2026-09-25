// Command bookforge manages projects and generates chapters until the model ends the book.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/RobiNexy/book-forge/internal/commands"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	project, configPath := ".", ""
	args, err := extractGlobal(args, &project, &configPath)
	if err != nil {
		return report(err, 64)
	}
	project, err = filepath.Abs(project)
	if err != nil {
		return report(err, 1)
	}
	if configPath == "" {
		configPath = filepath.Join(project, "bookforge.yaml")
	}
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 64
	}
	command, args := args[0], args[1:]
	switch command {
	case "init":
		dir := project
		if len(args) > 1 {
			return report(fmt.Errorf("init accepts at most one directory"), 64)
		}
		if len(args) == 1 {
			dir = args[0]
		}
		if err := commands.Init(dir); err != nil {
			return report(err, 73)
		}
	case "validate":
		if len(args) != 0 {
			return report(fmt.Errorf("validate accepts no arguments"), 64)
		}
		if err := commands.Validate(configPath); err != nil {
			return report(err, classify(err))
		}
	case "plan":
		if len(args) != 0 {
			return report(fmt.Errorf("plan accepts no arguments"), 64)
		}
		if err := commands.Plan(configPath); err != nil {
			return report(err, classify(err))
		}
	case "generate", "gen":
		fs := flag.NewFlagSet("generate", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		auto := fs.Bool("auto", false, "skip interactive review")
		noAudit := fs.Bool("no-audit-full", false, "do not save complete prompts and responses")
		model := fs.String("model", "", "override the configured model for this run")
		if err := fs.Parse(args); err != nil {
			return 64
		}
		if fs.NArg() != 0 {
			return report(fmt.Errorf("generate accepts no positional arguments"), 64)
		}
		if err := commands.Generate(ctx, configPath, *auto, *noAudit, *model); err != nil {
			return report(err, classify(err))
		}
	case "rewrite":
		generation, auto, err := parseRewriteArgs(args)
		if err != nil {
			return report(err, 64)
		}
		if err := commands.Rewrite(ctx, configPath, generation, auto); err != nil {
			return report(err, classify(err))
		}
	case "status":
		if len(args) != 0 {
			return report(fmt.Errorf("status accepts no arguments"), 64)
		}
		if err := commands.Status(configPath); err != nil {
			return report(err, classify(err))
		}
	case "log":
		if len(args) != 0 {
			return report(fmt.Errorf("log accepts no arguments"), 64)
		}
		if err := commands.Log(configPath); err != nil {
			return report(err, classify(err))
		}
	case "clear":
		if len(args) != 0 {
			return report(fmt.Errorf("clear accepts no arguments"), 64)
		}
		if err := commands.Clear(configPath); err != nil {
			return report(err, classify(err))
		}
	case "hooks":
		if len(args) < 1 || args[0] != "show" || len(args) > 2 {
			return report(fmt.Errorf("usage: bookforge hooks show [generation]"), 64)
		}
		generation := 0
		if len(args) == 2 {
			generation, err = strconv.Atoi(args[1])
			if err != nil || generation < 0 {
				return report(fmt.Errorf("generation must be a non-negative integer"), 64)
			}
		}
		if err := commands.Hooks(configPath, generation); err != nil {
			return report(err, classify(err))
		}
	case "render":
		if len(args) != 0 {
			return report(fmt.Errorf("render accepts no arguments"), 64)
		}
		if err := commands.Render(ctx, configPath); err != nil {
			return report(err, classify(err))
		}
	case "help", "--help", "-h":
		printUsage(os.Stdout)
	default:
		return report(fmt.Errorf("unknown command %q", command), 64)
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return 2
	}
	return 0
}

func parseRewriteArgs(args []string) (int, bool, error) {
	auto := false
	var positional []string
	for _, arg := range args {
		if arg == "--auto" {
			auto = true
		} else if strings.HasPrefix(arg, "-") {
			return 0, false, fmt.Errorf("unknown rewrite option %q", arg)
		} else {
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return 0, false, fmt.Errorf("usage: bookforge rewrite <generation> [--auto]")
	}
	generation, err := strconv.Atoi(positional[0])
	if err != nil || generation < 1 {
		return 0, false, fmt.Errorf("generation must be a positive integer")
	}
	return generation, auto, nil
}

func extractGlobal(args []string, project, configPath *string) ([]string, error) {
	remaining := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--project" || arg == "--config" {
			if i+1 >= len(args) {
				return nil, fmt.Errorf("%s requires a value", arg)
			}
			if arg == "--project" {
				*project = args[i+1]
			} else {
				*configPath = args[i+1]
			}
			i++
			continue
		}
		if strings.HasPrefix(arg, "--project=") {
			*project = strings.TrimPrefix(arg, "--project=")
			continue
		}
		if strings.HasPrefix(arg, "--config=") {
			*configPath = strings.TrimPrefix(arg, "--config=")
			continue
		}
		remaining = append(remaining, arg)
	}
	return remaining, nil
}

func classify(err error) int {
	if errors.Is(err, context.Canceled) {
		return 2
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "review aborted"), strings.Contains(message, "not committed"), strings.Contains(message, "cancelled"):
		return 2
	case strings.Contains(message, "api key"), strings.Contains(message, "openai"), strings.Contains(message, "generate chapter"):
		return 69
	case strings.Contains(message, "outline"), strings.Contains(message, "hook"), strings.Contains(message, "input"), strings.Contains(message, "config"), strings.Contains(message, "manifest"), strings.Contains(message, "marker"):
		return 65
	default:
		return 1
	}
}

func report(err error, code int) int {
	fmt.Fprintln(os.Stderr, "Error:", err)
	return code
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, `bookforge <command> [options]

Commands:
  init [dir]                create a project scaffold
  validate                  check config and readable text inputs
  plan                      preview the next numbered output file
  generate [--auto]         generate until the end-of-book marker
  rewrite N [--auto]        cascade rewrite from generation N to the end marker
  status                    print progress and completion state as JSON
  log                       print operation history
  hooks show [N]            print an opaque hook snapshot
  clear                     remove BookForge-generated data after confirmation
  render                    invoke the configured Quarto project

Global options: --project DIR, --config FILE`)
}
