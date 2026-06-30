# Link Resolution

The resolver is the third pipeline stage. It takes broken links from the validator and attempts to find the correct replacement URL using local git history, GitHub API, or AI.

## Strategy order

| Strategy | When used | Confidence |
|----------|-----------|------------|
| `git-history` | Broken relative link; local repo scanned for renames/deletions | High |
| `local-clone` | Broken GitHub link; target repo found under `--repos-dir` | High |
| `github-api` | Broken GitHub link; no local clone available | High |
| `ai` | Any external broken link; `--ai` flag set; all other strategies failed | Low |

## Usage

```sh
go run ./cmd/broken-links-parser/ extract \
  --root ~/Documents/GitHub/gardener/documentation \
  --dirs website/documentation \
  --validate \
  --resolve \
  --repos-dir ~/Documents/GitHub \
  --html reports/report.html
```

The report gains **Fixed Link** and **Strategy** columns. AI suggestions are shown with a yellow badge.

## Local clone detection

When `--repos-dir` is set, the resolver checks for the target repo at:
1. `<repos-dir>/<owner>/<repo>` (e.g. `~/Documents/GitHub/gardener/gardener`)
2. `<repos-dir>/<repo>` (e.g. `~/Documents/GitHub/gardener`)

If found, it runs `git log` locally — faster and rate-limit-free.

Before scanning, the tool runs `git fetch --quiet origin` to ensure the history is current. Skip with `--no-fetch` if you're offline or know the clone is up to date.

## Unresolved reason codes

When no fix can be found, the report shows an italicised reason in the Fixed Link column explaining why.

| Code | Meaning | How to fix |
|------|---------|------------|
| `No history found` | The file has no traceable history — it may never have existed at that path, or the link was a typo | Check the URL manually |
| `API blocked (token policy)` | The GitHub API returned 403. Most commonly caused by a fine-grained PAT whose lifetime exceeds the limit set by the repo's organisation (e.g. SAP enterprise enforces ≤366 days). Classic PATs are not subject to this restriction. | Switch to a classic PAT with `public_repo` scope at github.com/settings/tokens |
| `API rate limited` | The GitHub API rate limit was exhausted (60 req/hr unauthenticated, 5000/hr authenticated) after retries | Add or rotate `GITHUB_TOKEN` in `.env`; re-run with lower `--concurrency` |
| `Repo not found or private` | The GitHub API returned 404 — the org/repo may have been deleted, renamed, or made private | Check if the repo still exists; update the link manually |
| `Ambiguous (multiple matches)` | Multiple files with the same name exist in the current repo tree and commit history could not identify the exact rename target | Check the repo manually to determine which file is the intended target |
| `External link (enable --ai)` | The link points to a non-GitHub URL and `--ai` is not enabled, so no programmatic strategy is available | Re-run with `--ai` to attempt AI-assisted resolution, or fix manually |
| `AI returned no suggestion` | `--ai` was enabled but Claude returned no candidates | Check the URL manually |
| `AI suggestions did not pass validation` | Claude returned candidates but all failed HTTP validation | The candidates are visible in the report; check them manually |
| `AI returned an invalid URL` | Claude returned a syntactically malformed URL | Try enabling a stronger model |
| `Source URL is malformed` | The original broken URL is itself syntactically invalid — AI resolution was skipped | Fix the URL syntax in the source file directly |

### Note on GitHub token types

Fine-grained PATs (the newer token format) are subject to organisation-level policies. SAP's enterprise, for example, forbids tokens with a lifetime over 366 days. This causes 403 errors on any `gardener/*` or other SAP-hosted public repo.

**Recommendation:** Use a classic PAT (`github.com/settings/tokens → Tokens (classic)`) with `public_repo` scope. Classic PATs have no organisation-enforced expiry and work across all public repos.



AI uses the Claude API and is opt-in only. It is never called unless `--ai` is set. Suggestions are marked with `Confidence: low` and are not auto-applied unless `--apply-ai` is also set (reserved for the repair stage).

Requires `ANTHROPIC_API_KEY` in `.env` or the environment.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--resolve` | `false` | Run resolution after validation |
| `--repos-dir` | — | Directory containing local repo clones |
| `--no-fetch` | `false` | Skip `git fetch` on local clones |
| `--ai` | `false` | Enable AI-assisted resolution for external links |
| `--apply-ai` | `false` | Allow AI suggestions to be applied by the repair stage |
