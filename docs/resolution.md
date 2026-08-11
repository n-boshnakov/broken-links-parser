# Link Resolution

The resolver is the third pipeline stage. It takes broken links from the validator and attempts to find the correct replacement URL using local git history, the GitHub API, AI, or the Wayback Machine.

## Strategy order

| Strategy | When used | Confidence |
|----------|-----------|------------|
| `git-history` | Broken relative, anchor, or image link; local repo scanned for renames/deletions | High |
| `local-clone` | Broken absolute GitHub link; target repo found under `--repos-dir` | High |
| `github-api` | Broken absolute GitHub link; no local clone available | High |
| `wayback+ai` | External non-GitHub link; `--ai` and `--wayback` set; Wayback snapshot used to enrich the AI prompt | Low |
| `ai` | External non-GitHub link; `--ai` set; all other strategies failed | Low |

GitHub Enterprise links (any host with a configured token — see [validation.md](validation.md#authenticating-github-requests)) are treated like `github.com` links and go through the `local-clone` and `github-api` strategies. The reason strings in the table below are the human-readable labels shown in the report.

For GitHub links, the resolver checks rename history before concluding a file was deleted — a case-only rename (e.g. `FAQ.md` → `faq.md`) is correctly resolved as a rename, not a deletion.

## Usage

Full pipeline with AI and Wayback enrichment:

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

The report gains **Fixed Link** and **Strategy** columns.

## Local clone detection and auto-cloning

The resolver looks for a local clone of the target repo in this order:

1. `<repos-dir>/<owner>/<repo>` and `<repos-dir>/<repo>` (user-managed clones, highest priority)
2. `<cache-dir>/<owner>/<repo>` (auto-managed cache)
3. If not found in either, automatically clone into `--cache-dir` (unless `--no-cache` is set)
4. GitHub API (fallback when cache is disabled or clone fails)

When a clone is found or auto-cloned, `git fetch --quiet origin` runs before scanning to ensure the history is current. Skip with `--no-fetch` if you're offline.

### Auto-cloning (`--cache-dir`)

The tool automatically clones any GitHub repo it needs for resolution into a persistent cache directory (default `~/.cache/broken-links-parser/clones`). Clones use `--filter=blob:none --no-single-branch` — the full commit history is downloaded (needed for rename/deletion detection) while file contents (blobs) are fetched on demand.

**First run:** expect 10–30 s per new repo being cloned. For a docforge manifest referencing 20 repos, the first run may add several minutes. Subsequent runs are fast — only `git fetch` is needed.

**Disk space:** blobless clones of large repos are tens of MB each. 20 Gardener repos ≈ 500 MB–1 GB total in the cache.

Use `--no-cache` to disable auto-cloning and fall back to the GitHub API.

## AI resolution (`--ai`)

When `--ai` is set, the resolver uses an AI model as a last resort for external non-GitHub links that programmatic strategies couldn't resolve.

- Asks for up to 3 candidate URLs with confidence scores (0.0–1.0)
- Sorts candidates by confidence and HTTP-validates each in order
- Returns the first candidate that responds with a 2xx or 3xx status
- Rejects Wayback Machine URLs and other archive links as candidates
- 403/429 responses (bot-blocked sites) skip AI entirely — the page likely works in a browser

Requires `AI_API_KEY` in the environment or `.env`. Supports both Anthropic's API and any OpenAI-compatible proxy (LiteLLM, Azure OpenAI, etc.) via `AI_BASE_URL`.

## Wayback Machine enrichment (`--wayback`)

When `--wayback` is set alongside `--ai`, the resolver enriches the AI prompt with archived page context before calling the model.

**What it does for each broken external link:**

1. Queries the Wayback CDX API for the closest archived snapshot (5 s timeout)
2. If a usable snapshot exists (2xx archived status), fetches it and extracts the page title and first ~500 chars of body text (3 s timeout)
3. Checks whether the root domain is still alive — if so, tells the AI the content may have moved there
4. Injects all context into the AI prompt so the model knows *what* the page was about, not just its URL
5. If the AI finds no valid live replacement, the Wayback snapshot URL is returned as a last-resort fallback, labelled `"No live replacement found — see archived version"`

**Cost:** Up to ~8 s additional latency per broken external link. Skipped gracefully on timeout or unavailability.

**When it helps most:** Dead personal blogs, small documentation sites, moved resources. Less useful for large platforms (GitHub, Wikipedia) where other strategies already apply first.

## Unresolved reason codes

When no fix can be found, the report shows an italicised reason in the Fixed Link column.

| Reason | Meaning | How to fix |
|--------|---------|------------|
| `No history found` | File not in git history — may never have existed at that path, or a typo | Check the URL manually |
| `API blocked (token policy)` | GitHub API returned 403 — fine-grained PAT lifetime exceeds org policy (SAP: ≤366 days) | Switch to a classic PAT with `public_repo` scope |
| `API rate limited` | GitHub API rate limit exhausted after retries | Rotate `GITHUB_TOKEN`; re-run with lower `--concurrency` |
| `Repo not found or private` | GitHub API returned 404 — repo deleted, renamed, or made private | Check manually |
| `No local clone and API unavailable` | No local clone was found and the GitHub API could not be reached | Provide `--repos-dir`/`--cache-dir`, or check connectivity |
| `Ambiguous (multiple matches)` | Multiple files share the same name; commit history couldn't identify the exact rename | Check the repo manually |
| `External link (enable --ai)` | Non-GitHub URL and `--ai` not set | Re-run with `--ai`, or fix manually |
| `Bot-blocked (403/429) — likely works in browser` | Server blocks automated requests; page probably exists | Verify in a browser |
| `AI returned no suggestion` | AI returned no candidates | Check manually; resource may be gone |
| `AI suggestions did not pass validation` | AI returned candidates but all failed HTTP validation | Inspect the report manually |
| `AI returned an invalid URL` | AI returned a syntactically malformed URL | Try a stronger model |
| `AI auth error (check AI_API_KEY)` | AI API returned 401/403 — key missing, invalid, or expired | Check `AI_API_KEY` in `.env` |
| `Source URL is malformed` | The broken URL is itself syntactically invalid | Fix the URL syntax in the source file |

## AI provider configuration

The tool supports both Anthropic's native API and any OpenAI-compatible proxy.

| Variable | Default | Description |
|----------|---------|-------------|
| `AI_API_KEY` | — | API key for the AI provider |
| `AI_BASE_URL` | — | Leave unset for Anthropic; set to proxy base URL for LiteLLM/Azure/etc. |
| `AI_MODEL` | `claude-haiku-4-5-20251001` (Anthropic) / `gpt-4o-mini` (proxy) | Model to use |

Example for the SAP LiteLLM proxy:
```
AI_API_KEY=sk-your-key
AI_BASE_URL=https://models.answering-machine.utility.gardener.cloud.sap
AI_MODEL=claude-sonnet-4-6
```

### Note on GitHub token types

Fine-grained PATs are subject to organisation-level policies — SAP's enterprise forbids tokens with lifetime over 366 days, causing 403 errors on `gardener/*` repos.

**Recommendation:** Use a classic PAT (`github.com/settings/tokens → Tokens (classic)`) with `public_repo` scope. No organisation-enforced expiry, works across all public repos.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--resolve` | `false` | Run resolution after validation |
| `--repos-dir` | — | Directory containing local repo clones (checked before cache) |
| `--cache-dir` | `~/.cache/broken-links-parser/clones` | Directory for auto-cloned repos |
| `--no-cache` | `false` | Disable auto-cloning; fall back to GitHub API |
| `--no-fetch` | `false` | Skip `git fetch` on local and cached clones |
| `--github-tokens` | — | Explicit per-host token env var mapping (see [validation.md](validation.md#authenticating-github-requests)); enables GitHub Enterprise resolution |
| `--ai` | `false` | Enable AI-assisted resolution for external links |
| `--wayback` | `false` | Enrich AI prompts with Wayback Machine context; use archive as fallback (requires `--ai`) |
