// Command bookforge manages projects and generates chapters until the model ends the book.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
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
		return initCommand(args, project, os.Stdin, os.Stdout)
	case "validate":
		if len(args) != 0 {
			return report(fmt.Errorf("validate accepts no arguments"), 64)
		}
		if err := commands.Validate(configPath); err != nil {
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
	case "forge":
		if len(args) != 0 {
			return report(fmt.Errorf("forge accepts no arguments"), 64)
		}
		if err := commands.Forge(ctx, configPath); err != nil {
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

type initOption struct {
	value, label, detail string
}

var initFontOptions = []initOption{
	{"b", "本机基础字体", "按当前系统选择：Linux Noto CJK、Windows 宋体/微软雅黑、macOS 宋体 SC/PingFang"},
	{"a", "跨平台基础字体", "TeX Live 自带 Fandol + TeX Gyre，适合跨系统协作"},
	{"c", "典藏高配字体", "EB Garamond + Source Han Serif SC / Source Han Sans SC；需自行安装"},
	{"d", "现代高配字体", "IBM Plex + Source Han Serif SC / Source Han Sans SC；需自行安装"},
}

var initDesignOptions = []initOption{
	{"atelier", "Atelier · 文艺编辑", "暖纸色、陶土侧栏、衬线字母标记"},
	{"swiss", "Swiss · 瑞士网格", "朱红几何块、现代无衬线、清晰网格"},
	{"archive", "Archive · 典藏书系", "象牙纸、双细框、古典居中章题"},
	{"nocturne", "Nocturne · 午夜夜航", "深蓝底色、轨道线、金色点睛"},
	{"jade", "Jade · 东方青绿", "玉色纸面、错位留白、朱红印章"},
	{"", "清简经典", "保留基础书籍版式，不添加专属封面设计"},
}

func initCommand(args []string, project string, input io.Reader, output io.Writer) int {
	if len(args) > 1 {
		return report(fmt.Errorf("init accepts at most one directory"), 64)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			return report(fmt.Errorf("init is interactive; template flags are no longer supported"), 64)
		}
	}
	if len(args) == 1 {
		project = args[0]
	}
	reader := bufio.NewReader(input)
	font, err := chooseInitOption(reader, output, "选择字体方案", initFontOptions, 0)
	if err != nil {
		return report(err, 2)
	}
	design, err := chooseInitOption(reader, output, "选择书籍设计", initDesignOptions, 0)
	if err != nil {
		return report(err, 2)
	}
	fmt.Fprintf(output, "\n字体方案：%s\n书籍设计：%s\n\n", font.label, design.label)
	if err := commands.InitWithDesign(project, font.value, design.value); err != nil {
		return report(err, 73)
	}
	return 0
}

func chooseInitOption(reader *bufio.Reader, output io.Writer, question string, options []initOption, defaultIndex int) (initOption, error) {
	fmt.Fprintf(output, "? %s\n", question)
	for i, option := range options {
		marker := " "
		if i == defaultIndex {
			marker = "*"
		}
		fmt.Fprintf(output, "  %s %d. %s — %s\n", marker, i+1, option.label, option.detail)
	}
	for {
		fmt.Fprintf(output, "选择 [回车使用 %d]: ", defaultIndex+1)
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return initOption{}, fmt.Errorf("模板选择已取消：没有收到输入")
		}
		selection := strings.TrimSpace(line)
		if selection == "" {
			return options[defaultIndex], nil
		}
		choice, parseErr := strconv.Atoi(selection)
		if parseErr == nil && choice >= 1 && choice <= len(options) {
			return options[choice-1], nil
		}
		fmt.Fprintf(output, "请输入 1 到 %d 之间的编号，或直接按回车。\n", len(options))
		if err != nil {
			return initOption{}, fmt.Errorf("模板选择已取消：读取输入失败")
		}
	}
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
  init [dir]                create a project scaffold with interactive choices
  validate                  check config and readable text inputs
  generate [--auto]         generate until the end-of-book marker
  rewrite N [--auto]        cascade rewrite from generation N to the end marker
  log                       print operation history
  hooks show [N]            print an opaque hook snapshot
  clear                     remove BookForge-generated data after confirmation
  forge                     synchronize chapters and compile the Quarto book (PDF)

Init asks for a font profile and a book design; press Enter to accept defaults.
Global options: --project DIR, --config FILE`)
}
