# Link Validation

The validator is the second pipeline stage. It takes the links found by the extractor and checks whether each one is reachable, producing a result with a status and reason code.

## How each link type is checked

| Link type | Check |
|-----------|-------|
| `relative` / `anchor` (local path) | File existence via `os.Stat`; heading anchor presence for `#fragment` links |
| `image` (local path) | File existence via `os.Stat` |
| `image` (absolute URL) | HTTP HEAD request — badge SVGs and remote images are checked via HTTP |
| `absolute` | HTTP HEAD request, falling back to GET on `405 Method Not Allowed` (or when the server rejects HEAD) |
| `/`-prefixed paths | Resolved against `--root-relative-base` if set, else the repo root (or origin repo root for docforge-sourced files) — see [Root-relative links](#root-relative-links) |
| `mailto:` / `tel:` / other non-HTTP schemes | Skipped — marked as Ignored |

Local link targets are resolved with static-site route conventions: a link that
isn't a file as-written and doesn't already end in `.md`/`.html` is also tried as
`<target>.md`, `<target>/index.md`, and `<target>/_index.md` (and a directory target
is satisfied by an `index.md`/`_index.md` inside it). This resolves rendered routes
such as `/docs/foo/` → `docs/foo.md`. Markdown link titles (`[text](url "Title")`)
are stripped from the URL before checking.

Absolute links (including HTTP(S) image URLs) are checked concurrently (default 5 workers); relative and anchor links are checked synchronously. Progress is printed to stdout roughly every 50 links, with a running ETA.

A response with status 200–399 is treated as valid (redirects are followed by the HTTP client, and a 3xx that isn't followed is still counted as reachable); 400 and above is a broken `HTTP_ERROR`. Transient failures (`429`, `502`, `503`, `504`) are retried up to twice with exponential backoff and jitter before being reported.

HTTP requests include a browser-like `User-Agent` header to reduce false positives from basic bot protection. Requests to GitHub hosts are authenticated when a matching token is configured — see [Authenticating GitHub requests](#authenticating-github-requests).

## Anchor validation

For links with a `#fragment`, the validator extracts all headings from the target file and normalises them to GitHub-flavored anchor IDs:

- Markdown link syntax (`[text](url)`) stripped — only the text is used
- Backtick code spans unwrapped: `` `code` `` → `code`
- Orphaned `](url)` suffixes stripped (e.g. from headings that are themselves links)
- Lowercase, spaces → hyphens, non-alphanumeric characters removed
- Removing a special character can leave a double hyphen behind, and it is kept (e.g. `Shoot & Seed` → `shoot--seed`, because the `&` is dropped and both surrounding spaces become hyphens)
- Duplicate headings generate numbered variants: `foo`, `foo-1`, `foo-2`
- GitHub line-range anchors (`#L48-L55`) always treated as valid

When `ANCHOR_NOT_FOUND`, the report shows the closest matching anchor as a clickable suggestion.

## Root-relative links

Documentation sites commonly write links relative to the **published site root** rather
than the repository layout — e.g. `/docs/foo/`, `/adopter/`, `/images/diagram.svg`.
On disk those targets live under a content directory (such as `hugo/content`), not at
the repo root, so resolving them against the repo root reports thousands of spurious
`FILE_NOT_FOUND`s.

Use `--root-relative-base <dir>` to point `/`-prefixed links at the content root:

```sh
--root ~/repo --dirs hugo/content \
--root-relative-base ~/repo/hugo/content
```

With that set, `/adopter/images/teaser.svg` resolves to
`~/repo/hugo/content/adopter/images/teaser.svg`. Relative links (`./x.md`, `../y.md`)
are unaffected — they always resolve against the file that contains them. When the flag
is omitted, `/`-links fall back to the previous behaviour (repo root, or origin repo
root for docforge-sourced files).

Resolution is generic across static-site generators (Hugo, Jekyll, VitePress,
Docusaurus, MkDocs): a route like `/docs/foo/` matches `docs/foo.md`,
`docs/foo/index.md`, or `docs/foo/_index.md`.

## Reason codes

| Code | Meaning |
|------|---------|
| `FILE_NOT_FOUND` | Resolved local path does not exist |
| `ANCHOR_NOT_FOUND` | Target file exists but contains no heading matching the fragment; closest match shown |
| `HTTP_ERROR` | HTTP response was 4xx or 5xx, or the request failed at the transport level (status code shown in the report when available) |
| `TIMEOUT` | HTTP request exceeded the configured timeout |
| `IGNORED` | URL matched an ignore pattern (`.linkignore`, `--ignore-pattern`, `--ignore-file`, or `--scoped-ignore-file`) or uses a non-HTTP scheme — not validated |

### On false positives from bulk runs

When scanning thousands of links, some servers (kubernetes.io, goreportcard.com, certain GitHub endpoints) rate-limit automated requests, returning timeouts or 4xx responses even for pages that load fine in a browser. Reduce false positives with:

```sh
--concurrency 2 --timeout 30s
```

## Authenticating GitHub requests

Unauthenticated GitHub requests are rate-limited aggressively, which shows up as false-positive broken links on large runs. Configure a token per host so requests are authenticated:

- **github.com** — set `GITHUB_TOKEN`.
- **GitHub Enterprise hosts** — set an env var per host using the convention `GITHUB_<HOST>_TOKEN`, where the hostname is uppercased and dots/hyphens become underscores. These are auto-discovered; no flag is needed.

```sh
# .env
GITHUB_TOKEN=ghp_...                  # → github.com
GITHUB_TOOLS_SAP_TOKEN=ghp_...        # → github.tools.sap
GITHUB_WDF_SAP_CORP_TOKEN=ghp_...     # → github.wdf.sap.corp
```

When the convention is ambiguous (a host with both hyphens and dots maps to the same env var name as another), map hosts to env var names explicitly with `--github-tokens`:

```sh
--github-tokens github.tools.sap=GITHUB_TOOLS_SAP_TOKEN,github.wdf.sap.corp=GITHUB_WDF_SAP_CORP_TOKEN
```

If a GitHub Enterprise host is encountered without a matching token, the tool warns once and continues with unauthenticated requests.

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
# Lines before the first section are global — applied to all link URLs
mailto:*

# [/absolute/path/to/repo] starts a repo-specific section
[/Users/you/Documents/GitHub/gardener/gardener]
/dev-setup/*
/example-only-path/*

[/Users/you/Documents/GitHub/kubernetes/documentation]
# bare lines match link URLs (link is still collected, just marked IGNORED)
/website/documentation/landscapes/*
# "source:" lines skip scanning source files/folders entirely (no links collected).
# Globs are written relative to the repo root.
source: hugo/content/community/reviews/*
source: hugo/content/community/mail/*

# [sources] is a global source-skip section (globs relative to --root)
[sources]
generated/*
```

- **Global patterns** (before any `[...]` section) match all link URLs regardless of source
- **Repo-specific bare patterns** match link URLs, but only for links sourced from that repo
- **`source: <glob>` lines** (inside a repo section) skip scanning the matching source
  files/folders entirely — no links are collected from them. This differs from URL
  patterns, which still collect and validate the link and merely mark it IGNORED. Globs
  are written relative to the repo root and take effect when `--root` is on that repo's
  path.
- **`[sources]` section** is the global equivalent of `source:` lines — globs relative to
  `--root`, applied to every scan.
- Repo paths must be absolute — relative paths would break if the file moves.
- Source-skip (both `source:` and `[sources]`) applies to the primary scanned tree only,
  not to docforge-sourced files.

Pass it with `--scoped-ignore-file <path>`. Note that this is a different, sectioned format from the flat root `.linkignore` that is auto-loaded — although the file may be named `.linkignore` too. When working with Gardener docs, keeping this sectioned file in the tool's own repo is the recommended setup.

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
| `--root-relative-base` | — | Directory that root-relative (`/...`) links resolve against; defaults to the repo root (see [Root-relative links](#root-relative-links)) |
| `--concurrency` | `5` | Max concurrent HTTP requests |
| `--timeout` | `15s` | Per-link HTTP request timeout |
| `--github-tokens` | — | Explicit per-host token env var mapping, e.g. `github.tools.sap=GITHUB_TOOLS_SAP_TOKEN` (comma-separated); overrides the auto-discovered convention |
| `--cache-file` | `~/.cache/broken-links-parser/validation.json` | Path to validation result cache |
| `--cache-ttl` | `24h` | How long cached results remain valid |
| `--no-validation-cache` | `false` | Disable validation caching for a fresh run |
