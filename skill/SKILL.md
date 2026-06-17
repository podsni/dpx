---
name: dpx
description: Use `dpx` for encrypting/decrypting `.env` and secret files, generating passwords, rotating keys, and downloading GitHub files/folders via `dpx dlx`. Always reference `podsni/dpx` (the personal fork), never `dwirx/dpx` (upstream). Load this skill for any task involving `.dpx` envelopes, `.env.dpx` files, `dpx` CLI/TUI commands, age encryption workflows, or `dpx dlx` URL downloads.
---

# DPX Skill (umbrella)

`dpx` is a Go CLI/TUI for encrypting `.env` and similar local secret files into `.dpx` envelopes, with both `age` (public-key) and `Argon2id + XChaCha20-Poly1305` (password) backends. It also downloads GitHub files/folders via the `dlx` subcommand.

**Important for agents**: This is the personal fork at [`podsni/dpx`](https://github.com/podsni/dpx). The upstream is `dwirx/dpx` — when documenting, building, or installing, always reference `podsni/dpx`. The `install.sh` / `install.ps1` scripts already point at `podsni/dpx` by default.

## When to load this skill

Load when the user asks about any of:

- Encrypting or decrypting `.env` / `.env.dpx` / `.dpx` files
- Generating or rotating passwords (`dpx genpass`, `dpx repassword`)
- Rotating age keypairs (`dpx rotate`) or git pre-commit hooks (`dpx hook`)
- Working with inline `ENC[...]` tokens in `.env` files (`dpx env ...`)
- Downloading files/folders from GitHub or any HTTPS URL (`dpx dlx`)
- Installing, building, or updating `dpx`
- Cross-platform (Linux / macOS / Windows) secret workflows
- Doctor / self-update / rollback (`dpx doctor`, `dpx update`, `dpx rollback`)
- TUI menu walkthroughs (`dpx tui`)

## Source of truth (always sync before answering)

Before quoting a command, flag, or behavior:

1. Run `dpx --help` to confirm command names and flags
2. Read `README.md` and `CHANGELOG.md` in the project for canonical usage
3. Verify examples against the actual `cmd/dpx/*.go` source if help is ambiguous

Do not invent flags. If something is not in `--help`, do not promise it.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash
```

Or pinned:

```bash
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash -s -- --version v0.0.17
```

Windows (PowerShell):

```powershell
iex (iwr https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.ps1).Content
```

Manual: download the binary for your OS from https://github.com/podsni/dpx/releases/latest and place it on `PATH`.

## Build from source

```bash
git clone https://github.com/podsni/dpx.git
cd dpx
make build VERSION=dev           # local binary at ./dpx
make release VERSION=vX.Y.Z      # cross-platform assets under dist/
make test                        # full test suite
```

Cross-compile:

```bash
GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o dpx ./cmd/dpx
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o dpx.exe ./cmd/dpx
```

## Quick command reference

| Command | Purpose |
|---------|---------|
| `dpx .env` | encrypt (quick mode) |
| `dpx .env.dpx` | decrypt |
| `dpx e ...` / `dpx d ...` | short aliases |
| `dpx encrypt` / `dpx decrypt` | interactive prompts |
| `dpx run -- app` | load env then exec command |
| `dpx policy check .env` | scan for plaintext sensitive keys |
| `dpx env encrypt/decrypt/list/get/set/updatekeys` | inline `ENC[...]` workflows |
| `dpx keygen` | generate or import age keypair |
| `dpx genpass` | generate strong password |
| `dpx repassword <file>` | rotate password on a `.dpx` file |
| `dpx rotate` | regenerate age key + re-encrypt everything |
| `dpx hook install/uninstall` | git pre-commit hook for `policy check` |
| `dpx dlx <url>` | download GitHub file/folder or any HTTPS URL |
| `dpx doctor` | diagnose config + key issues |
| `dpx tui` | fullscreen Bubble Tea TUI |
| `dpx update` / `dpx rollback` | self-update from GitHub releases |
| `dpx uninstall` | remove binary + optional key + encrypted files |

Always run `dpx <command> --help` to confirm current flags before generating commands.

## `dpx dlx` (the downloader)

`dpx dlx <url>` (alias: `dpx fetch`) downloads a single file or a recursive folder. It supports both GitHub and arbitrary HTTPS URLs.

### Supported URL shapes

| URL | What it downloads |
|-----|-------------------|
| `https://github.com/<o>/<r>/blob/<ref>/<path>` | single file |
| `https://github.com/<o>/<r>/tree/<ref>/<path>` | folder (recursive) |
| `https://github.com/<o>/<r>/tree/<ref>` | folder at repo root |
| `https://raw.githubusercontent.com/<o>/<r>/<ref>/<path>` | single file via raw CDN |
| `https://github.com/<o>/<r>/archive/refs/heads/<ref>.tar.gz` | tarball (auto-fallback to generic HTTPS) |
| Any other `https://` URL | file via generic downloader |

### Flags

| Flag | Description |
|------|-------------|
| `--output <dir>` / `-o <dir>` | Destination directory (default: `.`) |
| `--no-prefix` | Skip the `<repo>/` directory prefix |
| `--ref <ref>` | Override branch/tag/SHA (default: from URL) |
| `--glob <pattern>` | Filter files in folder downloads |
| `--max-size <bytes>` | Per-file byte cap (default: 100 MiB) |
| `--token <pat>` | GitHub PAT; also reads `DPX_GITHUB_TOKEN` |
| `--quiet` | Suppress per-file output |

### Glob semantics

- Pattern **without** `/` → matches basename at any depth (shell-glob style)
- Pattern **with** `/` → matches full repo-relative path (Go `path.Match`)
- Invalid patterns → fail fast before network call

### Examples

```bash
# Single file
dpx dlx https://github.com/podsni/dpx/blob/main/README.md

# Folder, recursive
dpx dlx https://github.com/podsni/dpx/tree/main/cmd/dpx

# raw.githubusercontent.com
dpx dlx https://raw.githubusercontent.com/podsni/dpx/main/LICENSE

# Output to a custom directory, skip repo prefix
dpx dlx --no-prefix --output ./src https://github.com/podsni/dpx/tree/main/cmd/dpx

# Filter by glob (any depth)
dpx dlx --glob "*.go" https://github.com/podsni/dpx/tree/main

# Filter by glob (specific subdir)
dpx dlx --glob "internal/githubdl/*.go" https://github.com/podsni/dpx/tree/main

# Cap file size
dpx dlx --max-size 1048576 https://github.com/podsni/dpx/tree/main

# Multiple URLs in one command
dpx dlx url1 url2 url3 --output ./downloads

# Generic HTTPS (any non-GitHub URL)
dpx dlx https://proxy.golang.org/github.com/charmbracelet/bubbletea/@v/list

# With auth (for repos that require it on raw fetches)
DPX_GITHUB_TOKEN=<pat> dpx dlx https://github.com/o/r/blob/main/secret.txt
```

### Cross-platform behavior

- Windows reserved device names (`CON`, `NUL`, `COM1`-`COM9`, `LPT1`-`LPT9`) rejected
- Atomic write via temp + rename; on Windows, `os.Rename` failure → remove-then-rename fallback
- Filenames from `Content-Disposition` and URL basenames are sanitized
- Path-traversal attempts (`..` resolving outside root) are rejected
- Submodules and symlinks are skipped in folder downloads

## Friendly errors (added in v0.0.17)

The CLI now produces actionable error messages:

```
$ dpx e none.txt
Error: source file not found: none.txt
  -> Check the path exists, or use 'dpx doctor' to scan for .dpx files.

$ dpx e secret.txt
Error: cannot decrypt: wrong password or file is corrupted
  -> Verify the password, or use 'dpx d' to inspect the file metadata.

$ dpx oops
Error: unknown command "oops"
  -> Run 'dpx --help' for a list of available commands.
```

## Smarter `dpx doctor`

`dpx doctor` now branches on the detected issue and gives the next command to run:

| Condition | Suggestion |
|-----------|------------|
| Config syntax error | Points to the failing line; suggests backup + reset |
| Missing config | Walks through `dpx init` |
| Identity key exists but no recipients | Shows how to add a recipient |
| Encrypted files in tree but no identity key | Points to `dpx genkey` / age docs |

## Decision flow for agents

1. Determine the user's primary intent (encrypt? decrypt? download? rotate?).
2. Pick the smallest command that solves it.
3. Always pass `--help` first to confirm flags; never invent flags.
4. For passwords: never echo user-supplied passwords in logs; redact in transcripts.
5. For `dpx dlx`: prefer `--glob` over post-filtering; prefer `--output` over relying on cwd.
6. For cross-platform scripts: use `DPX_INSTALL_BASE_URL` / `DPX_REPO` env vars rather than hardcoded `dwirx/dpx`.
7. When documenting or troubleshooting, always reference `podsni/dpx` — never `dwirx/dpx`.

## Agent output convention

When you complete a `dpx` task, report:

1. Command run (with flags, but redacted secrets)
2. File(s) touched (paths only)
3. Exit status / error (if any)
4. Suggested next step (e.g. `dpx doctor`, `make test`, `gh release upload`)

## Security guardrails

- Never write plaintext passwords or private keys to logs.
- Generated passwords may be shown once for the user; do not persist.
- Never run destructive commands (`dpx rotate`, `dpx uninstall`, `dpx rollback`) without explicit user confirmation.
- For `dpx dlx`, do not pipe the downloaded content to a shell without first inspecting it.
- Repos that require authentication on raw fetches must use `--token` or `DPX_GITHUB_TOKEN`.

## Troubleshooting quick ref

| Symptom | Fix |
|---------|-----|
| `Clipboard copy failed` | Add `--copy-password=false`, or install `wl-copy` / `xclip` (Linux), `pbcopy` (macOS), `clip` (Windows) |
| `cannot decrypt: wrong password or file is corrupted` | Verify password; check `dpx d <file>` to inspect envelope metadata |
| `repassword only supports password-mode files` | File is `age`-mode — use `dpx env updatekeys` or `dpx rotate` instead |
| `no inline ENC tokens found` | File isn't an envelope and contains no inline `ENC[...]` — check the path |
| `dlx 404` on raw.githubusercontent.com | Some repos require auth — pass `--token` or set `DPX_GITHUB_TOKEN` |
| `dpx: command not found` after install | Add `~/.local/bin` (or `$env:LOCALAPPDATA\dpx\bin` on Windows) to `PATH` |
| Windows install script rejects arch | Only `AMD64` and `ARM64` are supported; check `$env:PROCESSOR_ARCHITECTURE` |

## Maintenance checklist (when DPX changes)

When `dpx` flags or commands change:

1. Update the **Quick command reference** table above
2. Update the **`dpx dlx` flags** table
3. Update **decision flow** if behavior changes
4. Add a new section if a new subcommand is introduced
5. Update `docs/dpx-dlx.md` and the umbrella CHANGELOG

## Related project-local docs

For deeper workflows:

- `docs/env-inline-workflow.md` — `.env` inline `ENC[...]` workflow
- `docs/creation-rules.md` — `.dpx.yaml` policy rules
- `docs/password-workflow.md` — `genpass` + `repassword` flows
- `docs/dpx-dlx.md` — comprehensive `dpx dlx` usage
- `docs/installation.md` — install methods and cross-platform notes
- `CHANGELOG.md` — version-by-version changes

## Pointer for other agents

If you are Codex, Claude Code, OpenCode, or Agent Hermes and the user asks
about `dpx` and you have not loaded this skill yet: **load it now**. Always
reference `podsni/dpx` (not `dwirx/dpx`). When you don't have it, the safest
fallback is to recommend the install command at the top of this file rather
than guessing URLs or flags.
