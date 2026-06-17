# `dpx dlx` — download files and folders from GitHub or any HTTPS URL

`dpx dlx` (alias: `dpx fetch`) downloads a single file or a recursive folder
with a single command. It supports both GitHub-specific layouts and arbitrary
HTTPS URLs.

This document is the comprehensive usage guide. The umbrella
[`skill/SKILL.md`](../skill/SKILL.md) has the short reference.

## Why

- One tool for "I want this file from this repo" without cloning the whole
  repo, `wget`-ing from `raw.githubusercontent.com`, or using `gh api`.
- Folder downloads preserve directory structure (with optional `<repo>/`
  prefix).
- Cross-platform from the start (Windows + Linux + macOS).
- Same flag set whether the URL is GitHub or not.

## Supported URL shapes

### GitHub UI (`github.com`)

| URL | Mode |
|-----|------|
| `https://github.com/<owner>/<repo>/blob/<ref>/<path>` | single file |
| `https://github.com/<owner>/<repo>/tree/<ref>/<path>` | folder (recursive) |
| `https://github.com/<owner>/<repo>/tree/<ref>` | folder at repo root |
| `https://github.com/<owner>/<repo>/archive/refs/heads/<ref>.tar.gz` | tarball (auto-fallback to generic HTTPS) |

### raw CDN (`raw.githubusercontent.com`)

| URL | Mode |
|-----|------|
| `https://raw.githubusercontent.com/<owner>/<repo>/<ref>/<path>` | single file |

### Generic HTTPS (fallback)

Anything else starting with `https://` is downloaded as a single file via the
generic downloader. `http://`, `ftp://`, `file://`, etc. are rejected.

Filename inference order:

1. `Content-Disposition: filename="..."` (RFC 6266)
2. `Content-Disposition: filename*=UTF-8''...` (RFC 5987)
3. URL basename
4. `download.bin`

## Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--output <dir>` | `-o` | `.` | Destination directory; created if missing |
| `--no-prefix` | | off | Skip the `<repo>/` directory prefix |
| `--ref <ref>` | | from URL | Override branch/tag/SHA |
| `--glob <pattern>` | | none | Filter files in folder downloads |
| `--max-size <bytes>` | | `104857600` (100 MiB) | Per-file byte cap |
| `--token <pat>` | | `$DPX_GITHUB_TOKEN` | GitHub PAT for rate-limit/auth-required repos |
| `--quiet` | | off | Suppress per-file output |

## Examples

### Single file

```bash
# Via GitHub UI
dpx dlx https://github.com/podsni/dpx/blob/main/README.md
# -> ./podsni/dpx/README.md

# Via raw CDN
dpx dlx https://raw.githubusercontent.com/podsni/dpx/main/LICENSE
# -> ./podsni/dpx/LICENSE

# Skip the <repo>/ prefix
dpx dlx --no-prefix https://github.com/podsni/dpx/blob/main/README.md
# -> ./README.md
```

### Folder (recursive)

```bash
# Whole folder
dpx dlx https://github.com/podsni/dpx/tree/main/cmd/dpx
# -> ./podsni/dpx/cmd/dpx/{main,genpass,repassword,rotate,hook,dlx}.go

# Custom output
dpx dlx --output ./src https://github.com/podsni/dpx/tree/main/cmd/dpx
# -> ./src/podsni/dpx/cmd/dpx/...

# At repo root
dpx dlx https://github.com/podsni/dpx/tree/main
# -> ./podsni/dpx/{.github,AGENTS.md,CHANGELOG.md,Makefile,README.md,...}
```

### Glob filter

```bash
# All .go files anywhere in the tree
dpx dlx --glob "*.go" https://github.com/podsni/dpx/tree/main

# Only top-level files in cmd/dpx
dpx dlx --glob "cmd/dpx/*.go" https://github.com/podsni/dpx/tree/main

# Pattern with character classes
dpx dlx --glob "main?.go" https://github.com/podsni/dpx/tree/main/cmd/dpx
```

Glob semantics:

- Pattern **without** `/` → matches the basename at any depth (shell-glob style)
- Pattern **with** `/` → matches the full repo-relative path (Go `path.Match`)
- Invalid patterns fail fast before any network call

### Size cap

```bash
# 1 MB per file
dpx dlx --max-size 1048576 https://github.com/podsni/dpx/tree/main

# No cap (set very high)
dpx dlx --max-size 1073741824 https://github.com/podsni/dpx/tree/main
```

### Multiple URLs

```bash
dpx dlx \
  https://github.com/podsni/dpx/blob/main/README.md \
  https://github.com/podsni/dpx/blob/main/CHANGELOG.md \
  --output ./downloads
```

Each URL is processed sequentially; the per-file output is grouped by URL.

### Generic HTTPS

```bash
# Tarball from a release
dpx dlx https://github.com/podsni/dpx/archive/refs/heads/main.tar.gz
# Auto-falls-back to generic HTTPS because the URL isn't a blob/tree layout

# Any CDN
dpx dlx https://proxy.golang.org/github.com/charmbracelet/bubbletea/@v/list
# -> ./list (700 B)
```

### Authentication (for repos that require it)

Some repos return `404` on unauthenticated raw fetches. Use a personal access
token (classic or fine-grained with `Contents: Read`):

```bash
# Inline
dpx dlx --token ghp_*** https://github.com/o/r/blob/main/secret.txt

# Or via env
export DPX_GITHUB_TOKEN=ghp_***
dpx dlx https://github.com/o/r/blob/main/secret.txt
```

## Cross-platform notes

| OS | Path handling | Filename restrictions |
|----|---------------|----------------------|
| Linux | POSIX-style (`/`) | None beyond general sanitization |
| macOS | POSIX-style (`/`) | Same as Linux |
| Windows | Native (`\`); forward-slash input converted via `filepath.FromSlash` | Windows reserved names rejected (`CON`, `NUL`, `COM1`-`COM9`, `LPT1`-`LPT9`); control chars rejected; `/` and `\` in server-supplied names sanitized |

Atomic write on all platforms:

- Write to `<name>.dpx-dlx-XXXXXXXX.tmp` in the destination directory
- `fsync` then `rename` to the final name
- On Windows where `os.Rename` refuses to overwrite: remove-then-rename
  fallback (best-effort)

## Security

- `SafeLocalPath` rejects:
  - `..` traversal that escapes the output root
  - Windows reserved device names
  - NUL bytes and other control characters
  - Path separators embedded in server-supplied filenames
- Atomic write prevents partial files on crash
- Auth tokens are sent only when explicitly provided

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `dlx 404` on raw.githubusercontent.com | Repo requires auth on raw fetches | Pass `--token` or set `DPX_GITHUB_TOKEN` |
| `unsupported github url kind` | URL is not `/blob/` or `/tree/` (e.g. archive, wiki, PR) | Re-run with the URL — auto-fallback to generic HTTPS will kick in |
| `invalid filename "CON"` (or other reserved name) | Server-supplied filename hits a Windows reserved name | Pass `--output` to a custom directory; file is skipped or replaced with a sanitized name |
| `file too large (exceeds N bytes)` | Hit the per-file cap | Pass `--max-size <larger>` |
| Rate-limit errors | Hit GitHub's 60 req/hr unauthenticated limit | Pass `--token` |

## See also

- [`skill/SKILL.md`](../skill/SKILL.md) — short reference for agents
- [`docs/installation.md`](installation.md) — install `dpx` itself
- [`CHANGELOG.md`](../CHANGELOG.md) — version-by-version notes
