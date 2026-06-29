# broken-links-parser

A CLI tool that scans Markdown and HTML documentation repositories for broken links, locates replacements via git history and the GitHub API, and repairs them in place.

## Pipeline

| Stage | Status | Doc |
|-------|--------|-----|
| **1. Extraction** — collect all links from `.md` and `.html` files | ✅ Done | [docs/extraction.md](docs/extraction.md) |
| **2. Validation** — check each link (disk / HTTP) | ✅ Done | [docs/validation.md](docs/validation.md) |
| **3. Resolution** — find correct replacement via git history / GitHub API / AI | ✅ Done | [docs/resolution.md](docs/resolution.md) |
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
```

See [docs/html-report.md](docs/html-report.md) for details on the interactive report.

## Run against gardener/documentation

```sh
go run ./cmd/broken-links-parser/ extract \
  --root ~/Documents/GitHub/gardener/documentation \
  --dirs website/documentation \
  --validate \
  --ignore-pattern "https://internal.*" \
  --html reports/report.html
```

## Run tests

```sh
go test ./...
```
