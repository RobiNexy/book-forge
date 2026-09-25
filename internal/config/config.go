// Package config loads the user-owned project manifest and applies documented defaults.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the normalized bookforge.yaml contract. All project paths are absolute after Load.
type Config struct {
	APIVersion string `yaml:"apiVersion"`
	Project    struct {
		Outline       string `yaml:"outline"`
		InitialHooks  string `yaml:"initial_hooks"`
		SystemPrompt  string `yaml:"system_prompt"`
		LongBookRules string `yaml:"long_book_rules"`
		ChaptersDir   string `yaml:"chapters_dir"`
		StateDir      string `yaml:"state_dir"`
	} `yaml:"project"`
	LLM struct {
		Provider        string            `yaml:"provider"`
		Model           string            `yaml:"model"`
		APIKeyEnv       string            `yaml:"api_key_env"`
		BaseURL         string            `yaml:"base_url"`
		Temperature     float64           `yaml:"temperature"`
		MaxOutputTokens int               `yaml:"max_output_tokens"`
		RequestTimeout  time.Duration     `yaml:"-"`
		TimeoutText     string            `yaml:"request_timeout"`
		Parameters      map[string]any    `yaml:"parameters"`
		Headers         map[string]string `yaml:"headers"`
	} `yaml:"llm"`
	Generation struct {
		Review                 string `yaml:"review"`
		StorePromptAndResponse bool   `yaml:"store_prompt_and_response"`
		InvalidOutputRetries   int    `yaml:"invalid_output_retries"`
	} `yaml:"generation"`
	Quarto struct {
		ProjectDir string `yaml:"project_dir"`
		Command    string `yaml:"command"`
	} `yaml:"quarto"`
	File string `yaml:"-"`
}

// Defaults returns the V1 defaults before any manifest values are applied.
func Defaults() Config {
	var c Config
	c.APIVersion = "bookforge/v1"
	c.Project.Outline = "outline.md"
	c.Project.InitialHooks = "initial_hooks.md"
	c.Project.SystemPrompt = "prompts/system.md"
	c.Project.LongBookRules = "prompts/long_book_gen.md"
	c.Project.ChaptersDir = "chapters"
	c.Project.StateDir = ".bookforge"
	c.LLM.Provider = "openai"
	c.LLM.Model = "gpt-4.1"
	c.LLM.APIKeyEnv = "OPENAI_API_KEY"
	c.LLM.BaseURL = "https://api.openai.com/v1"
	c.LLM.Temperature = 0.7
	c.LLM.MaxOutputTokens = 12000
	c.LLM.TimeoutText = "10m"
	c.LLM.RequestTimeout = 10 * time.Minute
	c.Generation.Review = "interactive"
	c.Generation.StorePromptAndResponse = true
	c.Generation.InvalidOutputRetries = 3
	c.Quarto.ProjectDir = "."
	c.Quarto.Command = "quarto"
	return c
}

// Load parses a manifest, applies defaults, resolves relative paths against its directory,
// and rejects unsupported or internally inconsistent settings before side effects occur.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read project config %q: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Config{}, fmt.Errorf("parse project config %q: %w", path, err)
	}
	if !hasRootKey(&document, "apiVersion") {
		return Config{}, fmt.Errorf("project config %q must declare apiVersion", path)
	}
	c := Defaults()
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("parse project config %q: %w", path, err)
	}
	c.File, err = filepath.Abs(path)
	if err != nil {
		return Config{}, err
	}
	if c.LLM.TimeoutText != "" {
		c.LLM.RequestTimeout, err = time.ParseDuration(c.LLM.TimeoutText)
		if err != nil {
			return Config{}, fmt.Errorf("invalid llm.request_timeout: %w", err)
		}
	}
	paths := []struct{ name, value string }{
		{"project.outline", c.Project.Outline}, {"project.initial_hooks", c.Project.InitialHooks},
		{"project.system_prompt", c.Project.SystemPrompt}, {"project.long_book_rules", c.Project.LongBookRules}, {"project.chapters_dir", c.Project.ChaptersDir},
		{"project.state_dir", c.Project.StateDir}, {"quarto.project_dir", c.Quarto.ProjectDir},
	}
	for _, path := range paths {
		if strings.TrimSpace(path.value) == "" {
			return Config{}, fmt.Errorf("%s must be non-empty", path.name)
		}
	}
	base := filepath.Dir(c.File)
	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(base, p)
	}
	c.Project.Outline = resolve(c.Project.Outline)
	c.Project.InitialHooks = resolve(c.Project.InitialHooks)
	c.Project.SystemPrompt = resolve(c.Project.SystemPrompt)
	c.Project.LongBookRules = resolve(c.Project.LongBookRules)
	c.Project.ChaptersDir = resolve(c.Project.ChaptersDir)
	c.Project.StateDir = resolve(c.Project.StateDir)
	c.Quarto.ProjectDir = resolve(c.Quarto.ProjectDir)
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func hasRootKey(document *yaml.Node, name string) bool {
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 {
		return false
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == name {
			return true
		}
	}
	return false
}

// Validate enforces configuration invariants after defaults and path normalization.
func (c Config) Validate() error {
	if c.APIVersion != "bookforge/v1" {
		return fmt.Errorf("unsupported apiVersion %q (expected bookforge/v1)", c.APIVersion)
	}
	if c.LLM.Provider != "openai" {
		return fmt.Errorf("unsupported llm.provider %q (V1 supports openai)", c.LLM.Provider)
	}
	if c.LLM.Model == "" || c.LLM.APIKeyEnv == "" || c.LLM.BaseURL == "" {
		return fmt.Errorf("llm.model, llm.api_key_env, and llm.base_url must be non-empty")
	}
	endpoint, err := url.Parse(c.LLM.BaseURL)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Host == "" {
		return fmt.Errorf("llm.base_url must be an absolute HTTP or HTTPS URL")
	}
	if c.LLM.MaxOutputTokens <= 0 || c.LLM.RequestTimeout <= 0 {
		return fmt.Errorf("llm.max_output_tokens and llm.request_timeout must be positive")
	}
	if c.LLM.Temperature < 0 || c.LLM.Temperature > 2 {
		return fmt.Errorf("llm.temperature must be between 0 and 2")
	}
	if c.Generation.Review != "interactive" && c.Generation.Review != "auto" {
		return fmt.Errorf("generation.review must be interactive or auto")
	}
	if c.Generation.InvalidOutputRetries < 0 {
		return fmt.Errorf("generation.invalid_output_retries cannot be negative")
	}
	if c.Quarto.Command == "" {
		return fmt.Errorf("quarto.command must be non-empty")
	}
	return nil
}
