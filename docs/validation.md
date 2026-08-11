# Link Validation

The validator is the second pipeline stage. It takes the links found by the extractor and checks whether each one is reachable, producing a result with a status and reason code.

## How each link type is checked

| Link type | Check |
|-----------|-------|
| `relative` / `anchor` (local path) | File existence via `os.Stat`; heading anchor presence for `#fragment` links |
| `image` (local path) | File existence via `os.Stat` |
| `image` (absolute URL) | HTTP HEAD request — badge SVGs and remote images are checked via HTTP |
| `absolute` | HTTP HEAD request, falling back to GET on 405 |
| `/`-prefixed paths | Resolved from the repo root (or origin repo root for docforge-sourced files) |
| `mailto:` / `tel:` / other non-HTTP schemes | Skipped — marked as Ignored |

Absolute links are checked concurrently (default 5 workers). Relative links are checked synchronously. Progress is printed to stdout with a per-link ETA during validation.

HTTP requests include a browser-like `User-Agent` header to reduce false positives from basic bot protection. GitHub URLs use an authenticated request when `GITHUB_TOKEN` is set.

## Anchor validation

For links with a `#fragment`, the validator extracts all headings from the target file and normalises them to GitHub-flavored anchor IDs:

- Markdown link syntax (`[text](url)`) stripped — only the text is used
- Backtick code spans unwrapped: `` `code` `` → `code`
- Orphaned `](url)` suffixes stripped (e.g. from headings that are themselves links)
- Lowercase, spaces → hyphens, non-alphanumeric characters removed
- Double hyphens preserved (e.g. `Shoot & Seed` → `shoot--seed`)
- Duplicate headings generate numbered variants: `foo`, `foo-1`, `foo-2`
- GitHub line-range anchors (`#L48-L55`) always treated as valid

When `ANCHOR_NOT_FOUND`, the report shows the closest matching anchor as a clickable suggestion.

## Reason codes

| Code | Meaning |
|------|---------|
| `FILE_NOT_FOUND` | Resolved local path does not exist |
| `ANCHOR_NOT_FOUND` | Target file exists but contains no heading matching the fragment; closest match shown |
| `HTTP_ERROR` | HTTP response was 4xx or 5xx (status code shown in report) |
| `TIMEOUT` | HTTP request exceeded the configured timeout |
| `IGNORED` | URL matched an `--ignore-pattern` or is a non-HTTP scheme — not validated |

### On false positives from bulk runs

When scanning thousands of links, some servers (kubernetes.io, goreportcard.com, certain GitHub endpoints) rate-limit automated requests, returning timeouts or 4xx responses that work fine in a browser. Reduce false positives with:

```sh
--concurrency 2 --timeout 30s
```

## Usage

Add `--validate` to any `extract` run:

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --validate \
  --html reports/report.html
```

Broken links are highlighted in red in the HTML report, with the HTTP status code shown (e.g. `HTTP_ERROR 404`).

### Skipping URLs with `.linkignore`

The tool automatically loads `.linkignore` from the root of the scanned repository — no flag needed. Add patterns there and they apply on every run.

`.linkignore` format — one pattern per line, `#` for comments:
```
# skip mailto links
mailto:*

# skip internal SAP URLs
https://*.internal.*
https://*.corp.*

# skip deliberately placeholder paths (never exist in the repo)
/dev-setup/*
```

You can also pass extra patterns via flag (repeatable) or point to an additional file:
```sh
--ignore-pattern "https://example.com/specific-page"
--ignore-file /path/to/extra.linkignore
```

Both are merged with `.linkignore` when present.

### Scoped ignore file (`--scoped-ignore-file`)

For distributed documentation spanning multiple repos, use a single sectioned ignore file that lives in the tool's own repo. Patterns are scoped to specific origin repos, so rules for one repo don't affect others.

Format:

```
# Lines before the first section are global — applied to all links
mailto:*

# [/absolute/path/to/repo] starts a repo-specific section
[/Users/you/Documents/GitHub/gardener/gardener]
/dev-setup/*
/example-only-path/*

[/Users/you/Documents/GitHub/kubernetes/kubernetes]
/staging/*
```

- **Global patterns** (before any `[...]` section) apply to all links regardless of source
- **Repo-specific patterns** apply only to links sourced from that repo's local clone
- Paths must be absolute — relative paths would break if the file moves

Pass it with `--scoped-ignore-file .linkignore` (the `.linkignore` in this repo is the recommended location when working with Gardener docs).

## Validation result cache

HTTP validation is the bottleneck — thousands of links at low concurrency takes 15–20 minutes. Most links are stable between runs. The tool caches validation results to disk so unchanged links are returned instantly on subsequent runs.

**First run:** no speedup — all links are validated via HTTP and results are saved.  
**Subsequent runs:** only new or expired links make HTTP requests. A typical re-run takes under a minute.

The cache is stored at `~/.cache/broken-links-parser/validation.json` by default. Only absolute URL results are cached — relative and anchor links depend on local file state and are always re-checked.

Use `--cache-ttl 1h` for a shorter TTL if you want fresher results. Use `--no-validation-cache` to disable caching entirely for a clean run.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--validate` | `false` | Run validation after extraction |
| `--ignore-pattern` | — | Skip links matching this glob (repeatable) |
| `--ignore-file` | — | Path to a file with ignore patterns (one per line, `#` = comment) |
| `--scoped-ignore-file` | — | Path to a sectioned ignore file with per-repo patterns (see above) |
| `--concurrency` | `5` | Max concurrent HTTP requests |
| `--timeout` | `15s` | Per-link HTTP request timeout |
| `--cache-file` | `~/.cache/broken-links-parser/validation.json` | Path to validation result cache |
| `--cache-ttl` | `24h` | How long cached results remain valid |
| `--no-validation-cache` | `false` | Disable validation caching for a fresh run |
