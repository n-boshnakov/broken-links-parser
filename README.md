# broken-links-parser

A CLI tool that scans Markdown and HTML documentation repositories for broken links, locates replacements via git history, the GitHub API, AI, and the Wayback Machine.

## Pipeline

| Stage | Status | Doc |
|-------|--------|-----|
| **1. Extraction** — collect all links from `.md` and `.html` files | ✅ Done | [docs/extraction.md](docs/extraction.md) |
| **2. Validation** — check each link (disk / HTTP); result cache for fast re-runs | ✅ Done | [docs/validation.md](docs/validation.md) |
| **3. Resolution** — find correct replacement via git history / GitHub API / AI / Wayback | ✅ Done | [docs/resolution.md](docs/resolution.md) |
| **4. Repair** — rewrite broken links in source files | 🔲 Planned | — |
| **5. Reporting** — CSV export + docforge-compatible log | 🔲 Planned | — |

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
  --docforge-manifest ~/Documents/GitHub/gardener/documentation/.docforge/website.yaml \
  --repos-dir ~/Documents/GitHub \
  --scoped-ignore-file .linkignore \
  --validate \
  --resolve \
  --ai \
  --wayback \
  --concurrency 2 --timeout 30s \
  --html reports/report.html
```

`--docforge-manifest` includes remote-sourced files from linked repos (e.g. `gardener/gardener`) without running a build. On first run, repos not found under `--repos-dir` are auto-cloned into `~/.cache/broken-links-parser/clones`. Validation results are cached at `~/.cache/broken-links-parser/validation.json` — subsequent runs complete in seconds. `--concurrency 2 --timeout 30s` reduces false positives caused by rate limiting on large runs.

Requires `GITHUB_TOKEN` and `AI_API_KEY` in `.env`. See [docs/resolution.md](docs/resolution.md) for token setup.

For internal GitHub Enterprise hosts, add a token per host using the naming convention `GITHUB_<HOST>_TOKEN`, where the hostname is uppercased and dots/hyphens replaced with underscores. These are auto-discovered — no flag needed:

```sh
# .env
GITHUB_TOKEN=ghp_...                  # → github.com
GITHUB_TOOLS_SAP_TOKEN=ghp_...        # → github.tools.sap
GITHUB_WDF_SAP_CORP_TOKEN=ghp_...     # → github.wdf.sap.corp
```

If the naming convention is ambiguous (hyphens vs dots both become `_`), use the `--github-tokens` flag to map hosts to env var names explicitly:

```sh
--github-tokens github.tools.sap=GITHUB_TOOLS_SAP_TOKEN,github.wdf.sap.corp=GITHUB_WDF_SAP_CORP_TOKEN
```

## Run tests

```sh
go test ./...
```

## Configuration

Copy `.env.example` (or create `.env`) with the following keys:

```
# GitHub token — classic PAT with public_repo scope recommended
GITHUB_TOKEN=ghp_...

# Additional GitHub Enterprise hosts: GITHUB_<HOST>_TOKEN convention
# Uppercase the hostname, replace dots and hyphens with underscores
GITHUB_TOOLS_SAP_TOKEN=ghp_...        # → github.tools.sap
GITHUB_WDF_SAP_CORP_TOKEN=ghp_...     # → github.wdf.sap.corp

# AI resolution (required only with --ai flag)
AI_API_KEY=sk-...
AI_BASE_URL=https://models.answering-machine.utility.gardener.cloud.sap  # optional: LiteLLM proxy
AI_MODEL=claude-sonnet-4-6  # optional: override default model
```
