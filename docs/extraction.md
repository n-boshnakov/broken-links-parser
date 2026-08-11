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

# Scan with docforge distributed documentation support
go run ./cmd/broken-links-parser/ extract \
  --root ~/Documents/GitHub/gardener/documentation \
  --dirs website/documentation \
  --docforge-manifest ~/Documents/GitHub/gardener/documentation/.docforge/website.yaml \
  --repos-dir ~/Documents/GitHub \
  --validate \
  --html reports/report.html
```

## Docforge distributed documentation (`--docforge-manifest`)

When a repository assembles documentation from multiple GitHub repos using [docforge](https://github.com/gardener/docforge) manifests, use `--docforge-manifest` to include those remote-sourced files in the scan.

The tool reads the manifest directly — no docforge build step required. For each remote-sourced file, it looks for a local clone under `--repos-dir` and extracts links from it. Relative links in sourced files are validated against their origin repo's filesystem.

Use `--docforge-strict` to additionally flag links that are valid in the origin repo but whose target is not included in the manifest — these will break after assembly.

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Repository root to scan |
| `--dirs` | all | Comma-separated subdirectories to restrict the scan |
| `--verbose` | `false` | Print each link to stdout |
| `--html` | — | Write an HTML report to this path |
| `--docforge-manifest` | — | Path to root docforge manifest YAML; includes remote-sourced files from local clones |
| `--docforge-strict` | `false` | Flag valid links whose target is not in the manifest (requires `--docforge-manifest`) |
