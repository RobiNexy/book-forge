package main

import (
	"context"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/commands"
	"github.com/RobiNexy/book-forge/internal/config"
	"os"
	"path/filepath"
	"strconv"
)

func main() { os.Exit(run()) }
func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: bookforge <init|gen|rewrite|render|status|hooks>")
		return 64
	}
	cmd := os.Args[1]
	project := "."
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--project" && i+1 < len(os.Args) {
			project = os.Args[i+1]
			i++
		}
	}
	project, _ = filepath.Abs(project)
	switch cmd {
	case "init":
		if len(os.Args) > 2 && os.Args[2][0] != '-' {
			project = os.Args[2]
		}
		if err := commands.Init(project); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 73
		}
		return 0
	case "status":
		if err := commands.Status(project); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "render":
		if err := commands.Render(context.Background(), project); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "hooks":
		actionIndex := 2
		if len(os.Args) <= actionIndex {
			return 64
		}
		action := os.Args[actionIndex]
		args := make([]string, 0, len(os.Args)-3)
		for i := actionIndex + 1; i < len(os.Args); i++ {
			if os.Args[i] == "--project" {
				i++
				continue
			}
			args = append(args, os.Args[i])
		}
		if err := commands.Hooks(project, action, args); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 64
		}
		return 0
	case "gen":
		extra := ""
		for i := 2; i+1 < len(os.Args); i++ {
			if os.Args[i] == "--config" {
				extra = os.Args[i+1]
			}
		}
		cfg := config.Load(project, extra, map[string]string{})
		from, to := 0, 0
		dry := false
		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--auto":
				cfg.Auto = true
			case "--dry-run":
				dry = true
			case "--from":
				i++
				from, _ = strconv.Atoi(os.Args[i])
			case "--to":
				i++
				to, _ = strconv.Atoi(os.Args[i])
			case "--model":
				i++
				cfg.Model = os.Args[i]
			case "--outline":
				i++
				cfg.Outline = os.Args[i]
			case "--initial-hooks":
				i++
				cfg.InitialHooks = os.Args[i]
			case "--no-audit-full":
				cfg.AuditFull = false
			}
		}
		if err := commands.Generate(context.Background(), project, cfg, from, to, dry); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	case "rewrite":
		if len(os.Args) < 3 {
			return 64
		}
		n, _ := strconv.Atoi(os.Args[2])
		cfg := config.Load(project, "", nil)
		if err := commands.Rewrite(project, n, cfg, true); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		return 64
	}
}
