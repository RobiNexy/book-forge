package commands

import (
	"embed"
	"fmt"
	"runtime"
	"strings"
)

//go:embed designs/*.tex
var designFiles embed.FS

type bookDesign struct {
	main, math, accent, muted string
}

var bookDesigns = map[string]bookDesign{
	"atelier":  {"TeX Gyre Pagella", "TeX Gyre Pagella Math", "934A35", "73685E"},
	"swiss":    {"TeX Gyre Heros", "TeX Gyre Termes Math", "C43B2D", "60656A"},
	"archive":  {"TeX Gyre Schola", "TeX Gyre Schola Math", "725A32", "746F64"},
	"nocturne": {"TeX Gyre Termes", "TeX Gyre Termes Math", "314568", "66758C"},
	"jade":     {"TeX Gyre Pagella", "TeX Gyre Pagella Math", "276454", "6B7A70"},
}

// Font profiles deliberately keep the PDF layout shared: switching profiles
// changes typography and accent without affecting generated chapter files.
type bookTemplate struct {
	name, description      string
	main, sans, mono, math string
	cjkSetup               string
	accent, muted, heading string
}

var bookTemplates = map[string]bookTemplate{
	"portable": {
		name: "portable", description: "通用基础 · TeX Live 自带 Fandol + TeX Gyre",
		main: "TeX Gyre Termes", sans: "TeX Gyre Heros", mono: "TeX Gyre Cursor", math: "TeX Gyre Termes Math",
		cjkSetup: "\\setCJKmainfont{FandolSong-Regular.otf}[BoldFont=FandolSong-Bold.otf,ItalicFont=FandolKai-Regular.otf]\n\\setCJKsansfont{FandolHei-Regular.otf}[BoldFont=FandolHei-Bold.otf]\n\\setCJKmonofont{FandolFang-Regular.otf}",
		accent:   "36526A", muted: "71808A", heading: "\\sffamily",
	},
	"linux": {
		name: "linux", description: "Linux 基础 · Noto CJK + TeX Gyre",
		main: "TeX Gyre Termes", sans: "TeX Gyre Heros", mono: "TeX Gyre Cursor", math: "TeX Gyre Termes Math",
		cjkSetup: "\\setCJKmainfont{Noto Serif CJK SC}\n\\setCJKsansfont{Noto Sans CJK SC}\n\\setCJKmonofont{Noto Sans Mono CJK SC}",
		accent:   "36526A", muted: "71808A", heading: "\\sffamily",
	},
	"windows": {
		name: "windows", description: "Windows · Microsoft YaHei + SimSun",
		main: "TeX Gyre Termes", sans: "TeX Gyre Heros", mono: "TeX Gyre Cursor", math: "TeX Gyre Termes Math",
		cjkSetup: "\\setCJKmainfont{SimSun}\n\\setCJKsansfont{Microsoft YaHei}\n\\setCJKmonofont{Microsoft YaHei}",
		accent:   "36526A", muted: "71808A", heading: "\\sffamily",
	},
	"macos": {
		name: "macos", description: "macOS · Songti SC + PingFang SC",
		main: "TeX Gyre Termes", sans: "TeX Gyre Heros", mono: "TeX Gyre Cursor", math: "TeX Gyre Termes Math",
		cjkSetup: "\\setCJKmainfont{Songti SC}\n\\setCJKsansfont{PingFang SC}\n\\setCJKmonofont{PingFang SC}",
		accent:   "36526A", muted: "71808A", heading: "\\sffamily",
	},
	"literary": {
		name: "literary", description: "典藏 · EB Garamond + 思源宋体 (install fonts)",
		main: "EB Garamond", sans: "Inter", mono: "JetBrains Mono", math: "STIX Two Math",
		cjkSetup: "\\setCJKmainfont{Source Han Serif SC}\n\\setCJKsansfont{Source Han Sans SC}\n\\setCJKmonofont{Source Han Sans SC}",
		accent:   "824F40", muted: "85766F", heading: "",
	},
	"modern": {
		name: "modern", description: "现代 · IBM Plex + 思源黑体 (install fonts)",
		main: "IBM Plex Serif", sans: "IBM Plex Sans", mono: "JetBrains Mono", math: "STIX Two Math",
		cjkSetup: "\\setCJKmainfont{Source Han Serif SC}\n\\setCJKsansfont{Source Han Sans SC}\n\\setCJKmonofont{Source Han Sans SC}",
		accent:   "136F72", muted: "658485", heading: "\\sffamily",
	},
}

func defaultBookTemplate(goos string) string {
	switch goos {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	default:
		return "linux"
	}
}

func renderBookTemplate(selection string) (string, string, error) {
	return renderBookDesign(selection, "")
}

// A design name can be used as a complete portable preset, or combined with
// an existing font profile through --design. No host fonts are needed by presets.
func renderBookDesign(selection, design string) (string, string, error) {
	selection = strings.ToLower(selection)
	design = strings.ToLower(design)
	if _, ok := bookDesigns[selection]; ok {
		if design != "" && design != selection {
			return "", "", fmt.Errorf("conflicting template %q and design %q", selection, design)
		}
		design, selection = selection, "portable"
	}
	if selection == "" || selection == "auto" {
		selection = defaultBookTemplate(runtime.GOOS)
	}
	selection = strings.ToLower(selection)
	aliases := map[string]string{"a": "portable", "b": defaultBookTemplate(runtime.GOOS), "c": "literary", "d": "modern"}
	if alias, ok := aliases[selection]; ok {
		selection = alias
	}
	p, ok := bookTemplates[selection]
	if !ok {
		return "", "", fmt.Errorf("unknown template %q (choose auto, a/b/c/d, linux/windows/macos, atelier/swiss/archive/nocturne/jade)", selection)
	}
	name, designTeX := p.name, ""
	if design != "" {
		d, ok := bookDesigns[design]
		if !ok {
			return "", "", fmt.Errorf("unknown design %q (choose atelier, swiss, archive, nocturne, jade)", design)
		}
		common, err := designFiles.ReadFile("designs/common.tex")
		if err != nil {
			return "", "", err
		}
		layout, err := designFiles.ReadFile("designs/" + design + ".tex")
		if err != nil {
			return "", "", err
		}
		designTeX = string(common) + "\n" + string(layout)
		p.accent, p.muted = d.accent, d.muted
		if p.name == "portable" {
			p.main, p.math = d.main, d.math
		}
		name += "/" + design
	}
	r := strings.NewReplacer(
		"__PROFILE__", name, "__MAIN__", p.main, "__SANS__", p.sans,
		"__MONO__", p.mono, "__MATH__", p.math,
		"__CJK_SETUP__", strings.ReplaceAll(p.cjkSetup, "\n", "\n          "),
		"__ACCENT__", "bookaccent", "__ACCENT_HEX__", p.accent,
		"__MUTED_HEX__", p.muted, "__HEADING__", p.heading,
		"__DESIGN__", strings.ReplaceAll(strings.TrimSpace(designTeX), "\n", "\n          "),
	)
	return r.Replace(string(quartoBookTemplate)), name, nil
}
