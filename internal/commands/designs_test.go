package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/store"
	"gopkg.in/yaml.v3"
)

var designNames = []string{"atelier", "swiss", "archive", "nocturne", "jade"}

func TestDesignerPresets(t *testing.T) {
	for _, design := range designNames {
		t.Run(design, func(t *testing.T) {
			text, name, err := renderBookTemplate(design)
			if err != nil || name != "portable/"+design {
				t.Fatalf("name=%q err=%v", name, err)
			}
			var document map[string]any
			if err := yaml.Unmarshal([]byte(text), &document); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(text, "__") || !strings.Contains(text, "FandolSong-Bold.otf") || !strings.Contains(text, "\\BookCoverStart") {
				t.Fatal("unresolved design or missing portable typography")
			}
			combined, _, err := renderBookDesign("c", design)
			if err != nil || !strings.Contains(combined, "EB Garamond") || !strings.Contains(combined, "\\BookCoverStart") {
				t.Fatalf("custom font combination failed: %v", err)
			}
		})
	}
	if _, _, err := renderBookDesign("auto", "invalid"); err == nil {
		t.Fatal("unknown design accepted")
	}
	if _, _, err := renderBookDesign("jade", "swiss"); err == nil {
		t.Fatal("conflicting design accepted")
	}
	dir := filepath.Join(t.TempDir(), "invalid")
	if err := InitWithDesign(dir, "auto", "invalid"); err == nil {
		t.Fatal("invalid design initialized")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("invalid design created project files")
	}
}

// Opt-in integration test: runs the real init -> state commit -> forge pipeline.
// Keep PDFs in the requested directory for visual inspection; each run gets fresh
// subdirectories so existing user projects are never overwritten.
func TestDesignerPDFs(t *testing.T) {
	root := os.Getenv("BOOKFORGE_RENDER_TEST_DIR")
	if root == "" {
		t.Skip("set BOOKFORGE_RENDER_TEST_DIR to render all five PDF specimens")
	}
	if !filepath.IsAbs(root) {
		t.Fatal("BOOKFORGE_RENDER_TEST_DIR must be absolute")
	}
	for _, tool := range []string{"quarto", "pdftotext"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, design := range designNames {
		t.Run(design, func(t *testing.T) {
			dir, err := os.MkdirTemp(root, design+"-")
			if err != nil {
				t.Fatal(err)
			}
			if err := InitWithTemplate(dir, design); err != nil {
				t.Fatal(err)
			}
			configFile := filepath.Join(dir, "_quarto.yml")
			data, err := os.ReadFile(configFile)
			if err != nil {
				t.Fatal(err)
			}
			text := strings.Replace(string(data), `title: "BookForge 书籍生成器"`, "title: \"时间的纹理：在复杂的世界中寻找一种清晰而从容的阅读方式\"\n  subtitle: \"文字、秩序与留白\"\n  output-file: specimen", 1)
			text = strings.Replace(text, "pdf-engine: xelatex", "pdf-engine: xelatex\n    latex-auto-install: false\n    latex-clean: false\n    keep-tex: true", 1)
			if err := os.WriteFile(configFile, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			index, err := os.ReadFile("testdata/design-index.qmd")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "index.qmd"), index, 0o644); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(dir, "bookforge.yaml")
			p, err := loadProject(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := p.store.Initialize(domain.ProjectState{}, p.inputs); err != nil {
				t.Fatal(err)
			}
			for i, file := range []string{"design-chapter.qmd", "design-long-title.qmd"} {
				chapter, err := os.ReadFile(filepath.Join("testdata", file))
				if err != nil {
					t.Fatal(err)
				}
				if err := p.store.CommitGeneration(i+1, string(chapter), domain.ProjectState{Finished: i == 1}, store.AuditRecord{Generation: i + 1, EndOfBook: i == 1}, "", "", false); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			if err := Forge(ctx, manifest); err != nil {
				t.Fatalf("render %s: %v (project: %s)", design, err, dir)
			}
			pdf := filepath.Join(dir, "_book", "specimen.pdf")
			output, err := exec.CommandContext(ctx, "pdftotext", "-layout", pdf, "-").CombinedOutput()
			if err != nil {
				t.Fatalf("pdftotext: %v\n%s", err, output)
			}
			for _, want := range []string{"时间的纹理", "文字、秩序与留白", "第一章 这是一个标题", "reading_time", "全书测试完成"} {
				if !strings.Contains(string(output), want) {
					t.Errorf("PDF missing %q: %s", want, pdf)
				}
			}
			if strings.Count(string(output), "第一章 这是一个标题") < 2 {
				t.Error("chapter spacing not preserved in both TOC and heading")
			}
			logs, err := filepath.Glob(filepath.Join(dir, "*.log"))
			if err != nil || len(logs) == 0 {
				t.Fatalf("expected retained LaTeX log: %v", err)
			}
			for _, log := range logs {
				data, err := os.ReadFile(log)
				if err != nil {
					t.Fatal(err)
				}
				for _, problem := range []string{"Missing character:", "Overfull \\hbox", "Overfull \\vbox", "undefined references"} {
					if strings.Contains(string(data), problem) {
						t.Errorf("%s in %s", problem, log)
					}
				}
			}
			t.Logf("PDF: %s", pdf)
		})
	}
}
