package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Model          string
	APIKeyEnv      string
	BaseURL        string
	Temperature    float64
	MaxTokens      int
	TimeoutSeconds int
	Auto           bool
	AuditFull      bool
	Outline        string
	InitialHooks   string
	ChaptersDir    string
}

func Default() Config {
	return Config{Model: "gpt-4o-2024-11-20", APIKeyEnv: "OPENAI_API_KEY", BaseURL: "https://api.openai.com/v1", Temperature: .8, MaxTokens: 8000, TimeoutSeconds: 300, AuditFull: true, Outline: "outline.md", InitialHooks: "initial-hooks.md", ChaptersDir: "chapters"}
}

// Load merges user config, project config, optional config, environment, then CLI overrides.
func Load(project, extra string, overrides map[string]string) Config {
	c := Default()
	files := []string{}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		files = append(files, filepath.Join(x, "bookforge", "config.yaml"))
	}
	files = append(files, filepath.Join(project, ".bookforge", "config.yaml"))
	if extra != "" {
		files = append(files, extra)
	}
	for _, f := range files {
		applyFile(&c, f)
	}
	if x := os.Getenv("BOOKFORGE_MODEL"); x != "" {
		c.Model = x
	}
	if x := os.Getenv("BOOKFORGE_BASE_URL"); x != "" {
		c.BaseURL = x
	}
	if x := os.Getenv("BOOKFORGE_API_KEY_ENV"); x != "" {
		c.APIKeyEnv = x
	}
	if x := overrides["model"]; x != "" {
		c.Model = x
	}
	if x := overrides["outline"]; x != "" {
		c.Outline = x
	}
	if x := overrides["initial-hooks"]; x != "" {
		c.InitialHooks = x
	}
	return c
}
func applyFile(c *Config, path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	section := ""
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			section = strings.TrimSuffix(line, ":")
			continue
		}
		p := strings.SplitN(line, ":", 2)
		if len(p) != 2 {
			continue
		}
		k, v := strings.TrimSpace(p[0]), strings.Trim(strings.TrimSpace(p[1]), "\"'")
		switch section + "." + k {
		case "llm.model":
			c.Model = v
		case "llm.api_key_env":
			c.APIKeyEnv = v
		case "llm.base_url":
			c.BaseURL = v
		case "llm.temperature":
			c.Temperature, _ = strconv.ParseFloat(v, 64)
		case "llm.max_tokens":
			c.MaxTokens, _ = strconv.Atoi(v)
		case "llm.timeout_seconds":
			c.TimeoutSeconds, _ = strconv.Atoi(v)
		case "generation.auto_mode":
			c.Auto = v == "true"
		case "generation.audit_full":
			c.AuditFull = v != "false"
		case "paths.outline":
			c.Outline = v
		case "paths.initial_hooks":
			c.InitialHooks = v
		case "paths.chapters_dir":
			c.ChaptersDir = v
		}
	}
}
