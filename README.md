# broken-links-parser

A CLI tool that scans Markdown and HTML documentation repositories for broken links, locates replacements via git history and the GitHub API, and repairs them in place.

## Current status

The **link extractor** stage is implemented. Validation, resolution, repair, and reporting are planned.

## Try it now

```sh
# Build
go build -o broken-links-parser ./cmd/broken-links-parser/

# Extract all links from a repository
./broken-links-parser extract --root /path/to/repo --verbose

# Restrict to specific subdirectories
./broken-links-parser extract --root /path/to/repo --dirs docs,website --verbose
```

Example output:
```
Found 42 links in /path/to/repo
  [relative] ../guide/intro.md  (docs/overview.md)
  [absolute] https://github.com/org/repo/blob/main/README.md  (docs/overview.md)
  [anchor] #installation  (docs/overview.md)
  [image] ./images/arch.png  (docs/overview.md)
```

## Run against a real repo

```sh
git clone https://github.com/gardener/documentation /tmp/gardener-docs
go run ./cmd/broken-links-parser/ extract --root /tmp/gardener-docs --dirs website/documentation --verbose
```

## Run tests

```sh
go test ./...
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--root` | `.` | Repository root directory to scan |
| `--dirs` | all | Comma-separated subdirectories to restrict the scan |
| `--verbose` | false | Print each link found |
