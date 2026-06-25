# HTML Report

Pass `--html <path>` to write an interactive HTML report after extraction or validation.

## Without validation

Shows all links found with type badges and a live filter/sort UI.

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --html reports/report.html
```

## With validation

Adds a Status column. Broken links are highlighted in red; the reason code is shown inline.
The header displays the total link count and broken count.

```sh
go run ./cmd/broken-links-parser/ extract \
  --root /path/to/repo \
  --validate \
  --html reports/report.html
```

## Features

- **Live filter** — type in the search box to filter rows by URL or source file
- **Sortable columns** — click any column header to sort ascending/descending
- **Clickable absolute URLs** — open in a new tab directly from the report
- **Type badges** — colour-coded: relative (blue), absolute (green), anchor (yellow), image (pink)
- **Status badges** — Valid (green), Broken (red), Ignored (grey)
