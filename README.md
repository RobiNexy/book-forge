# BookForge

[中文使用教程](README_zh.md)

BookForge is a Go CLI for generating Quarto chapters from two opaque text files: an outline and the current hook block. It does not parse chapter headings, chapter count, or hook structure. Generation continues until the model returns the explicit `<<<END_OF_BOOK>>>` marker.

## Requirements and build

- Go 1.22 or later to build from source.
- An OpenAI API key to generate chapters.
- Quarto installed to render a PDF.

```sh
go test ./...
go build -o ./bookforge ./cmd/bookforge
```

## Prepare a project

### Initialize

```sh
./bookforge init ./my-book
```

`init` creates missing files and never overwrites existing ones:

```text
my-book/
├── bookforge.yaml
├── outline.md
├── initial_hooks.md
├── prompts/
│   ├── system.md
│   └── long_book_gen.md
├── chapters/
└── .bookforge/
```

### Write the source text

Write any Markdown or plain text in `outline.md` and `initial_hooks.md`. BookForge passes each file's exact text to the model; headings, lists, and chapter boundaries are not interpreted.

```markdown
<!-- outline.md -->
# Story premise

The protagonist finds a letter predicting an event the following day.

Possible progression:
- the letter arrives
- the protagonist investigates its source
- the prediction comes true
```

```markdown
<!-- initial_hooks.md -->
## Unanswered questions
- Who wrote the letter?
- Why is it dated tomorrow?

Continuity note: the protagonist's sister disappeared three years ago.
```

`prompts/system.md` controls book style. `prompts/long_book_gen.md` provides the default long-form planning rules and can be edited independently. The system prompt sent to the model consists of the style file, long-book rules, and BookForge's fixed output protocol, in that order.

### Configure Quarto

BookForge does not create or edit `quarto.yml`, templates, or PDF layout settings. The user's Quarto book must list the generated files. For example, after three chapters have been generated, add them to the existing `quarto.yml` or `_quarto.yml`:

```yaml
project:
  type: book

book:
  title: My Book
  chapters:
    - index.qmd
    - chapters/001.qmd
    - chapters/002.qmd
    - chapters/003.qmd

format:
  pdf: default
```

Create `index.qmd` and maintain the chapter list yourself. BookForge checks that Quarto references every generated chapter before rendering.

## Generate until the book is complete

Commands default to the current directory. Select a project with `--project DIR`, or use `--config FILE` to specify a manifest. Relative paths in the manifest are resolved from its directory.

```sh
./bookforge --project ./my-book validate
./bookforge --project ./my-book plan
```

`validate` checks that configuration and source files are readable and existing state is consistent. It does not infer outline chapters or hook meaning. `plan` previews the next numbered output file.

Set the environment variable named by `llm.api_key_env` (default `OPENAI_API_KEY`), then start generation:

```sh
export OPENAI_API_KEY="your-api-key"
./bookforge --project ./my-book generate
```

Each request contains one system message with three ordered sections—`prompts/system.md`, `prompts/long_book_gen.md`, and the fixed protocol—and one user message containing the outline, current hooks, and an instruction to generate exactly one chapter. The chapter sequence number is used for filenames only; it is not sent to the model.

The model must return exactly one of these formats:

```text
Chapter prose...
<<<BOOKFORGE_HOOKS>>>
Complete hook text for the next generation.
```

```text
Final chapter prose...
<<<END_OF_BOOK>>>
```

The markers are mutually exclusive. Missing, repeated, or conflicting markers, or an empty chapter, make a response invalid. BookForge retries invalid output three times by default (four requests total); configure `generation.invalid_output_retries` to change the number of additional retries. Every invalid response is displayed and saved under `.bookforge/invalid-responses/`; exhausting retries fails without committing the chapter or state. Invalid responses are retained even when full prompt/response auditing is disabled. The hook text is opaque and replaces the previous hook state; it is never automatically appended to prior hook blocks.

Interactive review is the default:

- `a` — approve and commit;
- `A` — approve and automatically approve the rest of this run;
- `v` — view the complete chapter and next hook text;
- `e` — edit the chapter with `$EDITOR`, then confirm the edit;
- `r` — request a replacement response;
- `q` — quit without committing the current chapter.

Use `--auto` to skip review. Generation still validates markers and commits each chapter before requesting the next one:

```sh
./bookforge --project ./my-book generate --auto
```

The console shows each request and, after commit, a formatted chapter summary with the output path, hook/completion state, and a preview of up to the first 30 lines.

There is no generation-count limit. BookForge continues until `<<<END_OF_BOOK>>>` is committed. If interrupted, run `generate` again to resume from the latest complete chapter and hook snapshot. If the final marker was committed, it reports that the book is complete and exits.

## Rewrite, inspect, and render

```sh
./bookforge --project ./my-book status
./bookforge --project ./my-book log
./bookforge --project ./my-book hooks show
./bookforge --project ./my-book hooks show 2
./bookforge --project ./my-book rewrite 5
./bookforge --project ./my-book rewrite 5 --auto
./bookforge --project ./my-book clear
./bookforge --project ./my-book render
```

`rewrite N` first displays the resolved chapter file and asks for confirmation. It then archives generation N and every later chapter, snapshot, audit, and completion marker; restores the state after generation N−1; and generates until the model returns the end marker. `status` prints the chapter-to-file mapping, commit status, and completion state as JSON. `log` prints the JSONL operation history. `hooks show [N]` prints the uninterpreted hook snapshot after generation N; with no N it displays the latest state. `clear` lists generated files and state and asks for confirmation before removing only BookForge-managed artifacts. It preserves source text, prompts, `bookforge.yaml`, Quarto files, and unrecognized files; a fresh log entry records that the clear completed.

If `outline.md`, `initial_hooks.md`, `prompts/system.md`, or `prompts/long_book_gen.md` changes after generation starts, normal continuation is rejected. Use `rewrite 1` to archive the old chain, reset the initial hooks from the edited file, and generate again.

## Configuration

`bookforge.yaml` is the project manifest. See [`configs/config.example.yaml`](configs/config.example.yaml) for every supported field; `init` writes the V1 defaults.

| Setting | Purpose | Default |
|---|---|---|
| `project.outline` | Opaque outline text file | `outline.md` |
| `project.initial_hooks` | Opaque initial hooks text file | `initial_hooks.md` |
| `project.system_prompt` | Style and book-specific instructions | `prompts/system.md` |
| `project.long_book_rules` | Long-form generation rules | `prompts/long_book_gen.md` |
| `project.chapters_dir` | Numbered `.qmd` output | `chapters` |
| `project.state_dir` | Manifest, snapshots, and audits | `.bookforge` |
| `llm.provider` / `llm.model` | Model backend and name | `openai` / `gpt-4.1` |
| `llm.api_key_env` | API-key environment variable | `OPENAI_API_KEY` |
| `llm.base_url` | OpenAI-compatible endpoint | `https://api.openai.com/v1` |
| `llm.temperature`, `max_output_tokens`, `request_timeout` | Generation parameters | `0.7`, `12000`, `10m` |
| `llm.parameters` | Arbitrary model API fields passed through as-is | `{}` |
| `llm.headers` | Additional HTTP request headers | `{}` |
| `generation.review` | `interactive` or `auto` | `interactive` |
| `generation.store_prompt_and_response` | Save full prompt and raw response in audit | `true` |
| `generation.invalid_output_retries` | Additional retries after an invalid response | `3` |
| `quarto.project_dir` / `quarto.command` | Quarto working directory and executable | `.`, `quarto` |

CLI options `--auto`, `--model`, and `--no-audit-full` apply only to that run. `llm.parameters` keys and values are merged into the API request without BookForge interpreting their meaning; unsupported parameters are reported with the server's error response. API keys are never persisted. JSON snapshots and audit records are tool-managed; users do not need to write JSON.

For example, a provider-specific option can be configured without adding it to BookForge's schema:

```yaml
llm:
  parameters:
    reasoning_effort: high
```

Check that the selected model API supports each parameter; BookForge forwards values unchanged.

For endpoints that require additional HTTP headers, configure string key/value pairs:

```yaml
llm:
  headers:
    X-Api-Client: bookforge
```

Headers are forwarded as configured; a custom header with the same name as a default request header replaces the default value.

## State and boundaries

- `.bookforge/manifest.json` stores source hashes, not a generation counter.
- `.bookforge/snapshots/state_000.json` stores initial hooks. `state_NNN.json` stores the current opaque hook text and the end-of-book flag.
- `.bookforge/audit/run_NNN.json` stores per-generation metadata and optionally the full prompt and raw response.
- `.bookforge/invalid-responses/` retains malformed model responses for diagnosis regardless of the full-audit setting; `.bookforge/log.jsonl` records generation, review, retry, rewrite, clear, and render operations.
- `chapters/NNN.qmd`, the snapshot, and audit file together identify a committed generation. The next sequence is inferred from the contiguous artifacts.
- `<<<END_OF_BOOK>>>` is the sole completion condition and is persisted in the final snapshot.
- A project lock prevents concurrent generation or rendering in the same local project.
- Prompt-cache hits depend on the model service. BookForge places the outline first in the user prompt but cannot guarantee cache hits.

Design details are in [`docs/new_design.md`](docs/new_design.md). Development checks: `go test ./...`, `go test -race ./...`, and `go vet ./...`.
