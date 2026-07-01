# broken-links-parser

A CLI tool that scans Markdown and HTML documentation repositories for broken links, locates replacements via git history, the GitHub API, AI, and the Wayback Machine.

## Pipeline

| Stage | Status | Doc |
|-------|--------|-----|
| **1. Extraction** — collect all links from `.md` and `.html` files | ✅ Done | [docs/extraction.md](docs/extraction.md) |
| **2. Validation** — check each link (disk / HTTP) | ✅ Done | [docs/validation.md](docs/validation.md) |
| **3. Resolution** — find correct replacement via git history / GitHub API / AI / Wayback | ✅ Done | [docs/resolution.md](docs/resolution.md) |
| **4. Repair** — rewrite broken links in source files | 🔲 Planned | — |
| **5. Reporting** — CSV report + docforge-compatible log | 🔲 Planned | — |

## Quick start

```sh
# Build
go build -o broken-links-parser ./cmd/broken-links-parser/

# Extract all links from a repo
./broken-links-parser extract --root /path/to/repo --verbose

# Extract + validate + write HTML report
./broken-links-parser extract \
  --root /path/to/repo \
  --validate \
  --html reports/report.html

# Full pipeline with AI and Wayback enrichment
./broken-links-parser extract \
  --root /path/to/repo \
  --validate \
  --resolve \
  --ai \
  --wayback \
  --html reports/report.html
```

See [docs/html-report.md](docs/html-report.md) for details on the interactive report.

## Run against gardener/documentation

```sh
go run ./cmd/broken-links-parser/ extract \
  --root ~/Documents/GitHub/gardener/documentation \
  --dirs website/documentation \
  --validate \
  --resolve \
  --ai \
  --wayback \
  --repos-dir ~/Documents/GitHub \
  --html reports/report.html
```

Requires `GITHUB_TOKEN` and `AI_API_KEY` in `.env`. See [docs/resolution.md](docs/resolution.md) for token setup.

## Run tests

```sh
go test ./...
```

## Configuration

Copy `.env.example` (or create `.env`) with the following keys:

```
# GitHub token — classic PAT with public_repo scope recommended
GITHUB_TOKEN=ghp_...

# AI resolution (required only with --ai flag)
AI_API_KEY=sk-...
AI_BASE_URL=https://models.answering-machine.utility.gardener.cloud.sap  # optional: LiteLLM proxy
AI_MODEL=claude-sonnet-4-6  # optional: override default model
```
