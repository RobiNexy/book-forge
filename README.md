# BookForge

BookForge is a Go 1.22+ CLI that turns a Markdown outline into Quarto chapter files while maintaining a deterministic hook-pool snapshot after each chapter.

## Build and test

```sh
go test ./...
go build ./cmd/bookforge
```

## Quick start

```sh
bookforge init ./my-book
# Put ## chapter headings in my-book/outline.md.
# Optionally add one hook per line to initial-hooks.md.
bookforge gen --project ./my-book --dry-run
OPENAI_API_KEY=... bookforge gen --project ./my-book --auto
bookforge status --project ./my-book
bookforge hooks show --project ./my-book
bookforge render --project ./my-book
```

The API key is read only from the environment. State lives in `.bookforge/state`, audits in `.bookforge/audit`, and generated files in `chapters`. `gen` resumes from the latest complete snapshot; `rewrite N` truncates state from chapter N onward.

The V1 parser intentionally requires the response protocol documented in `docs/design.md`. Human review is represented by the audit mode, while `--auto` is the non-interactive production path.
