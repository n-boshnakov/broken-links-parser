# Evolution

How broken-links-parser came to be what it is. This document bridges the two
layers of the project's history: the git commit log (what shipped) and the
OpenSpec change proposals under `openspec/changes/archive/` (why each piece was
built, and what was deliberately left out). The OpenSpec folder is git-ignored,
so without this document that reasoning would be invisible to anyone reading
the repository alone.

The project was built with Claude Code using OpenSpec, a spec-driven
development workflow: every feature started as a proposal (why), became a design
(trade-offs), was broken into tasks (what ships), then was implemented and
archived. The specs double as requirements and as documentation.

## The original vision

The umbrella proposal (`2026-06-29-broken-links-parser`) framed the whole tool
before most of it existed. It imagined a pipeline of distinct stages:
extraction, validation, resolution, repair, and reporting, with a GitHub Action
wrapper for CI. Two of those original ideas did not survive contact with real
use, and that is worth recording honestly:

- **Repair was dropped.** The original plan had a stage that rewrote broken
  links in source files automatically. It was dropped by decision, not left
  unfinished. Low-confidence and AI-suggested fixes are common, and a silent
  bad rewrite degrades content worse than a flagged break does. The interactive
  HTML report makes applying a suggested fix trivial (copy the suggested URL,
  paste), so the resolver suggests and a human applies. A `--dry-run` plus diff
  preview is the review-first path if automated repair is ever revisited.
- **Reporting shipped as an interactive HTML report**, not the originally
  planned CSV export plus docforge-compatible log.

What remained is a four-stage pipeline, complete end to end: extract, validate,
resolve, report.

## Phase 1: Foundation (late June 2026)

The first working tool was assembled from three independent capabilities, each
specced as its own change and built in sequence:

- **Link extractor** (`2026-06-25-link-extractor`, commit `865f037`) — walk a
  documentation tree and capture every link with its type, anchor text, and
  byte offset in source. The byte offset was captured from the very first
  version to leave the door open for in-place repair later.
- **Link validator** (`2026-06-25-link-validator`, commit `eebc7b0`) — check
  each link: local paths on disk, anchors against target headings, HTTP links
  over the network. Added a link-ignore mechanism in the same step.
- **Link resolver** (`2026-06-25-link-resolver`, commit `0bcc63b`) — for a
  broken link, find where the target actually went using local git history
  (renames and deletions).

GitHub authentication and HTTP status surfacing landed alongside these
(`3440ce9`), and the three capabilities were then assembled into the single
`broken-links-parser` CLI under the umbrella proposal.

## Phase 2: Intelligence (end of June, early July 2026)

Programmatic resolution can only follow links it can trace. Two changes added
the ability to reason about links that left no local trail:

- **AI resolver** (`2026-06-30-link-resolver-ai`, commits `b4d0bb1`, `b68a063`
  via PR #1) — for broken absolute non-GitHub URLs, ask an AI model for
  candidate replacements, then HTTP-validate each candidate before trusting it.
- **Wayback-assisted resolution** (`2026-07-01-wayback-assisted-resolution`,
  commit `226358d`) — enrich the AI prompt with the closest Wayback Machine
  snapshot: the archived page's title and opening text tell the model what the
  page was actually about, and the archived URL itself is a last-resort fallback.

## Phase 3: Scale and robustness (July 2026)

With the core working, the focus shifted to making it usable against large,
real documentation sets rather than toy inputs. This was the busiest phase:

- **Distributed documentation / docforge** (`2026-07-02-distributed-documentation-docforge`,
  commit `3d4bfc9` via PR #3) — read docforge manifests directly and validate
  links across repository boundaries with no site build. This is the tool's
  real differentiator: in a distributed docs set a link in one repo can point
  into another, so links break silently across repo boundaries when files move.
- **Pipeline refactor** (`2026-07-02-refactor-pipeline`, `93d057e` via PR #2)
  — extract stage orchestration out of a monolithic `main.go` into a dedicated
  pipeline package.
- **Git-cache refactor** (`2026-07-01-refactor-git-cache`) — consolidate git
  access behind a cache so repeated history lookups stop re-shelling.
- **Auto-clone cache** (`2026-07-06-auto-clone-cache`, commit `000dbc7` via
  PR #4) — when a referenced repo has no local clone, clone it on demand
  (blobless, full history, blobs fetched as needed) so GitHub-link resolution
  works without manual setup.
- **Validation result cache** (`2026-07-06-auto-clone-cache` /
  `2026-07-09-cache-implementation`, commit `1d6c71d` via PR #5) — persist
  validation results to disk so re-runs finish in seconds instead of minutes.
- **Production-found fixes** (`2026-07-09-broken-links-issues`, commit `12ad33e`
  via PR #6) — a batch of logic improvements and corrections surfaced by running
  the tool against real documentation.

A run of suggestion-quality changes followed through July and August
(`9833d43`, `8a07b18`, `311d6e2`, `2a0d893`, `3af8074`), each tightening how
closely a suggested replacement had to match before the tool would offer it.

## Phase 4: Hardening (August to October 2026)

The final recorded phase made the tool dependable in the environment it was
built for and sharpened its weakest output:

- **Multi-host GitHub authentication** (`2026-08-06-add-internal-authentication`,
  commit `4f02a23` via PR #8) — support multiple GitHub hosts (github.com plus
  internal GitHub Enterprise instances) through per-host tokens discovered by
  naming convention, so resolution works across public and enterprise repos in
  one run.
- **Anchor suggestion improvements** (`2026-08-06-anchor-suggestion-improvements`)
  — close the gap where broken anchors produced no suggestion at all, by
  extracting HTML `id` attributes as anchor targets and matching on word-token
  overlap rather than exact string match.
- Continued suggestion-precision work and AI-key failure surfacing through the
  autumn (`436dd0a` via PR #13, `9b13e78` via PR #14), ending with the fixed-
  links location update (`79a192e`, 2026-10-21).

## In flight

The current unarchived change, `link-capture-resolve-issues-fix`, came out of
production testing against the Gardener documentation, which surfaced 23 distinct
issues across three tiers: extraction bugs (footnote-definition links, unbalanced
parentheses in URLs, image-plus-absolute duplicates, bare relative paths), anchor
suggestion gaps (fragment normalisation, Levenshtein threshold too tight for
common suffixes, hyphen-boundary prefix matching), and resolution correctness
(wrong path depth, false deletion detection on specific-SHA blob URLs, per-run
resolution-result caching so each broken URL is resolved only once). Most of it
is done and tested; the open items are manual spot-checks against specific
Gardener repos and a dedup check against a repo with badge SVGs.

## Why this document exists

The OpenSpec planning layer (`openspec/`) is git-ignored. The commit messages
record what changed but not why a stage was dropped or what a change was
reacting to. This document is the single place where those two threads are tied
together, so the shape of the tool, including the parts that were deliberately
not built, stays legible to anyone who only has the repository in front of them.
