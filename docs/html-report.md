# HTML Report

Pass `--html <path>` to write an interactive HTML report after any pipeline stage.

## Without validation

Shows all links found with type badges and a live filter/sort UI.

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

## Features

- **Live filter** — type in the search box to filter rows by URL, source file, or reason
- **Sortable columns** — click any column header to sort ascending/descending
- **Clickable absolute URLs** — open in a new tab directly from the report
- **Type badges** — colour-coded: relative (blue), absolute (green), anchor (yellow), image (pink)
- **Status badges** — Valid (green), Broken (red), Ignored (grey)
- **Strategy badges** — git-history, github-api, local-clone (blue); AI / Wayback+AI (yellow)
