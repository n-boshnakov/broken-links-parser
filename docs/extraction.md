# Link Extraction

The extractor is the first pipeline stage. It walks `.md` and `.html` files in a repository and returns every link it finds, along with its type, the anchor/alt text, and the byte offset of the URL in the source file (used later by the repair stage).

## Link types

| Type | Example |
|------|---------|
| `relative` | `[guide](../guide/intro.md)` |
| `absolute` | `[repo](https://github.com/org/repo)` |
| `anchor` | `[see below](#installation)` |
| `image` | `![diagram](./images/arch.png)` |

HTML files (`.html`) are also scanned: `<a href>` and `<img src>` attributes are extracted, including `<a>` inner text and `<img alt>` text.

Reference-style Markdown links (`[text][ref]` + `[ref]: url`) are resolved and emitted as regular links pointing to the definition line.

Links inside fenced code blocks and inline code spans are ignored.

## Link text

For every link, the extractor captures the anchor or alt text alongside the URL:

- Markdown inline: `[Kubernetes concepts](url)` → `Text: "Kubernetes concepts"`
- Markdown image: `![architecture diagram](url)` → `Text: "architecture diagram"`
- HTML anchor: `<a href="url">Click here</a>` → `Text: "Click here"`
- HTML image: `<img src="url" alt="logo">` → `Text: "logo"`

This text is passed to the AI resolver as context when suggesting replacements for broken links.

## Usage

```sh
# Print a link count
go run ./cmd/broken-links-parser/ extract --root /path/to/repo

# Print every link found
go run ./cmd/broken-links-parser/ extract --root /path/to/repo --verbose

# Restrict to subdirectories
go run ./cmd/broken-links-parser/ extract --root /path/to/repo --dirs docs,website

# Write an HTML report
go run ./cmd/broken-links-parser/ extract --root /path/to/repo --html reports/report.html
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Repository root to scan |
| `--dirs` | all | Comma-separated subdirectories to restrict the scan |
| `--verbose` | `false` | Print each link to stdout |
| `--html` | — | Write an HTML report to this path |
