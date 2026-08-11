# HTML Report

Pass `--html <path>` to write an interactive HTML report after any pipeline stage.

## Without validation

Shows all links found with type badges and filter/sort controls.

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --html reports/report.html
```

## With validation

Adds a **Status** column. Broken links are highlighted in red with the HTTP status code (e.g. `HTTP_ERROR 404`). The header shows total link count and broken count.

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --validate \
  --html reports/report.html
```

## With resolution

Adds **Fixed Link** and **Strategy** columns.

- Fixed links are clickable
- Deleted files show `"Deleted in commit: <url>"` linking to the deletion commit
- Wayback fallbacks show `"No live replacement found — see archived version: <url>"`
- AI suggestions show a yellow `AI (low confidence)` or `Wayback + AI` badge
- Anchor suggestions show a grey `Closest match` badge — the nearest heading when `ANCHOR_NOT_FOUND`
- Unresolved links show an italicised reason explaining why no fix was found

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --validate \
  --resolve \
  --ai \
  --wayback \
  --html reports/report.html
```

## Filtering

The report has a filter bar above the table with three sets of toggle chips and a text search box. Filters combine: all active chips within a group use OR logic; groups use AND logic.

### Type chips

Toggle one or more link types to show only those rows:

| Chip | Shows |
|------|-------|
| `relative` | Relative file links (`../guide/intro.md`) |
| `absolute` | Absolute URLs (`https://...`) |
| `anchor` | Same-page anchor links (`#heading`) |
| `image` | Image links (`![alt](src)`) |

### Status chips *(available when `--validate` was used)*

| Chip | Shows |
|------|-------|
| `Broken` | Links that returned an error (4xx, 5xx, timeout, file not found) |
| `Valid` | Links that resolved successfully |
| `Ignored` | Links skipped due to `.linkignore` or `--ignore-pattern` |
| `Not in manifest` | Links valid on disk but not included in the docforge manifest (`--docforge-strict` only) |

### Fixed chips *(available when `--resolve` was used)*

| Chip | Shows |
|------|-------|
| `Has fix` | Broken links where a replacement URL was found |
| `Unresolved` | Broken links where no replacement could be found |
| `Deleted` | Links where the target file was deleted; points to the deletion commit |
| `Wayback fallback` | Links where no live replacement was found; archive URL provided |

### Text search

The search box filters by URL or source file path. It works alongside the chips — only rows that match both the active chips and the search text are shown.

### Clear all

The **Clear all** button resets all chips and the search box at once.

## Sorting

Click any column header to sort that column ascending. Click again to reverse. Sorting works on the currently visible rows (filtered results sort independently of hidden rows).

## Badges

| Badge | Colour | Meaning |
|-------|--------|---------|
| `relative` | Blue | Relative file link |
| `absolute` | Green | Absolute URL |
| `anchor` | Yellow | Anchor-only link |
| `image` | Pink | Image link |
| `Valid` | Green | Link is reachable |
| `Broken` | Red | Link returned an error |
| `Ignored` | Grey | Link was skipped |
| `Not in manifest` | Amber | Valid on disk but not assembled |
| `git-history` / `local-clone` / `github-api` | Blue | Programmatic fix found |
| `AI (low confidence)` / `Wayback + AI` | Yellow | AI-assisted suggestion |
| `Closest match` | Grey | Nearest heading anchor suggestion for `ANCHOR_NOT_FOUND` links |

## Source file column

Source files are shown as clickable GitHub URLs pointing to the file in the upstream repository — both for files in the primary scanned repo and for files pulled from remote repos via a docforge manifest.
