# Link Validation

The validator is the second pipeline stage. It takes the links found by the extractor and checks whether each one is reachable, producing a result with a status and reason code.

## How each link type is checked

| Link type | Check |
|-----------|-------|
| `relative` / `anchor` / `image` (local path) | File existence via `os.Stat`; heading anchor presence for `#fragment` links |
| `absolute` | HTTP HEAD request, falling back to GET on 405 |
| `mailto:` / `tel:` / other non-HTTP schemes | Skipped — marked as Ignored |

Absolute links are checked concurrently (default 5 workers). Relative links are checked synchronously.

HTTP requests include a browser-like `User-Agent` header to reduce false positives from basic bot protection. GitHub URLs use an authenticated request when `GITHUB_TOKEN` is set.

## Reason codes

| Code | Meaning |
|------|---------|
| `FILE_NOT_FOUND` | Resolved local path does not exist |
| `ANCHOR_NOT_FOUND` | Target file exists but contains no heading matching the fragment |
| `HTTP_ERROR` | HTTP response was 4xx or 5xx (status code shown in report) |
| `TIMEOUT` | HTTP request exceeded the configured timeout |
| `IGNORED` | URL matched an `--ignore-pattern` or is a non-HTTP scheme — not validated |

## Usage

Add `--validate` to any `extract` run:

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --validate \
  --html reports/report.html
```

Broken links are highlighted in red in the HTML report, with the HTTP status code shown (e.g. `HTTP_ERROR 404`).

### Skipping internal or VPN-only URLs

The tool automatically loads `.linkignore` from the root of the scanned repository if it exists — no flag needed. Add patterns there and they apply on every run.

`.linkignore` format — one pattern per line, `#` for comments:
```
# skip mailto links
mailto:*

# skip internal SAP URLs
https://*.internal.*
https://*.corp.*
```

You can also pass extra patterns via flag (repeatable) or point to an additional file:
```sh
--ignore-pattern "https://example.com/specific-page"
--ignore-file /path/to/extra.linkignore
```

Both are merged with `.linkignore` when present.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--validate` | `false` | Run validation after extraction |
| `--ignore-pattern` | — | Skip links matching this glob (repeatable) |
| `--ignore-file` | — | Path to a file with ignore patterns (one per line, `#` = comment) |
| `--concurrency` | `5` | Max concurrent HTTP requests |
| `--timeout` | `15s` | Per-link HTTP request timeout |
