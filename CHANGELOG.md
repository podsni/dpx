# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project follows [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [v0.0.17] - 2026-06-17

> Personal fork of [`dwirx/dpx`](https://github.com/dwirx/dpx) hosted at
> [`podsni/dpx`](https://github.com/podsni/dpx). All upstream history is preserved;
> entries below describe changes layered on top of `v0.0.16`.

### Added

- **`dpx dlx`** (alias: `fetch`) - download files and folders from GitHub, or
  any HTTPS URL, with a single command:
  - GitHub single-file: `dpx dlx https://github.com/<o>/<r>/blob/<ref>/<path>`
  - GitHub folder (recursive): `dpx dlx https://github.com/<o>/<r>/tree/<ref>/<path>`
  - GitHub raw URLs: `dpx dlx https://raw.githubusercontent.com/<o>/<r>/<ref>/<path>`
  - Release tarballs and non-standard `github.com` URLs auto-fall-back to
    generic HTTPS download
  - Any other HTTPS URL works via the generic downloader
  - Multiple URLs in one command: `dpx dlx url1 url2 url3 --output ./downloads`
  - Flags:
    - `--output <dir>` - output directory (default: `.`)
    - `--no-prefix` - skip the `<repo>/` directory prefix
    - `--ref <ref>` - override branch/tag (default: derived from URL)
    - `--glob <pattern>` - filter files in folder downloads
      (`*` without `/` matches basename at any depth; with `/` matches full
      repo-relative path via Go `path.Match`)
    - `--max-size <bytes>` - max bytes per file (default: `104857600`, 100 MiB)
    - `--token <pat>` - GitHub PAT (or env `DPX_GITHUB_TOKEN`)
    - `--quiet` - suppress per-file output
  - Filename inference: URL basename, `Content-Disposition: filename="..."`,
    `Content-Disposition: filename*=UTF-8''...` (RFC 6266), or `download.bin`
  - Submodules and symlinks are skipped to prevent directory-escape
- **`internal/httpdl` package** - generic HTTPS downloader with:
  - `Accept-Encoding: identity` to avoid surprise decompression
  - 5-minute timeout, configurable transport
  - Filename sanitization (control chars stripped, Windows reserved names
    rejected)
  - Atomic write via temp + rename (Windows fallback: remove-then-rename)
- **`internal/githubdl` package** - GitHub-aware downloader with:
  - URL parser handling `blob`, `tree`, and `raw.githubusercontent.com` shapes
  - `SafeLocalPath` rejects Windows reserved names, `..` traversal, and NUL bytes
  - Recursive `walkFolder` (depth-first pre-order) via GitHub Trees API
  - `countingReader` wrapper to work around Go transparently decompressing
    `Content-Length: -1` chunked responses
  - Atomic write with Windows-safe rename fallback
- **`dpx doctor`** now branches on the detected issue with concrete next steps:
  - Config syntax error - points to the failing line; suggests backup + reset
  - Missing config - walks through `dpx init`
  - Identity key exists but no recipients in config - shows how to add one
  - Encrypted files in tree but no identity key - points to `dpx genkey` / age docs

### Changed

- **Friendly CLI error messages** - hard-to-decipher Go errors now produce
  actionable hints with the next command to run:
  - `source file not found` - suggests `dpx doctor` or path check
  - `cannot decrypt: wrong password or file is corrupted` - suggests `dpx d`
    to inspect envelope metadata
  - `unknown command "<x>"` - suggests `dpx --help`
- **`dpx dlx` help** - cross-platform section added (Windows reserved names,
  atomic write fallback, `filepath.Join` usage, read-only bit on Windows).

### Fixed

- `SafeLocalPath` no longer strips the final path segment when its file is
  missing (regression from the initial implementation).
- URL parser now correctly handles `#` characters in GitHub paths.
- Folder downloads now apply the repo prefix consistently (previously only
  file downloads did).
- Size reporting now shows actual bytes instead of `?` for GitHub raw
  responses (chunked transfer + transparent decompression workaround).

### Security

- `SafeLocalPath` rejects:
  - Windows reserved device names (`CON`, `NUL`, `COM1`-`COM9`, `LPT1`-`LPT9`,
    plus the same with any extension)
  - Path traversal attempts (`..` resolving outside output root)
  - NUL bytes and other control characters
  - Path separators embedded in server-supplied filenames
- Atomic write prevents partial-file leakage on crash/interrupt
  (Windows: remove-then-rename fallback when `os.Rename` refuses overwrite).
- Filenames from `Content-Disposition` headers are sanitized before use.

### Tests

- **+86 new tests** since `v0.0.16`:
  - 31 in `internal/httpdl` (URL parsing, filename inference, redirects,
    Content-Disposition, override, path-safety, max-size, cancel)
  - 27 in `internal/githubdl/parse` (all URL shapes, error cases, glob
    semantics, Windows reserved names, traversal rejection)
  - 14 in `internal/githubdl/download` (mocked `httptest.Server`: file,
    folder, rate-limit, token header, repo prefix, traversal, cancel,
    size limit, atomic write)
  - 14 in `internal/tui` (coverage for previously uncovered TUI helpers:
    `encryptOptions`, `encryptScopeName`, `isEncryptedPath`, `splitCSV`,
    `parseEnvKeysInput`, `extractAgeSecretKey`, `filterPathsByQuery`)
- **254 tests total** (`go test -race ./...`), race-clean, vet-clean, gofmt-clean.

## [v0.0.16] - 2026-03-20

### Added
- New `dpx repassword` command to rotate password encryption for:
  - password-mode `.dpx` envelopes
  - inline password tokens (`ENC[...]`) in env-style files
- New `dpx genpass` command (alias: `passgen`) to generate strong passwords from CLI.
- New TUI actions:
  - `Repassword (Manual)`
  - `Repassword (Generate)`
  - `Generate Password`
- New documentation:
  - `docs/password-workflow.md`
  - `docs/agent-skill-usage.md`
  - `docs/testing-report-2026-03-20.md`
  - `skill/SKILL.md`

### Changed
- Upgraded `genpass` with better generation options:
  - `--count` to generate multiple passwords in one command
  - `--no-symbols` for compatibility-focused output
  - entropy estimation summary in output
- Removed automatic clipboard copy behavior from generated-password flows.
- Updated README and CLI help text for the new password-generation and repassword workflows.

### Fixed
- Updated TUI doctor test input to match new menu ordering after adding password actions.

## [v0.0.15] - 2026-03-20

### Added
- `dpx rotate`: New command to seamlessly regenerate age key pairs and re-encrypt all `.dpx` files and inline secrets.
- `dpx hook install`/`uninstall`: New command to manage a Git pre-commit hook that prevents accidental commits of plaintext secrets using `dpx policy check`.
- TUI integration for `Regenerate Key`, `Git Hook Install`, and `Git Hook Uninstall` with elegant sub-handlers to safely execute terminal-heavy operations.
- Expanded `README` documentation for key rotation and git pre-commit hooks.

### Changed
- Strengthened the interactive warning prompt for destructive key rotation operations.

## [v0.0.14] - 2026-03-19

### Fixed
- Fullscreen TUI no longer exits when typing/pasting `q` inside input fields.
- Windows import-key paste flow is now stable for age key blocks containing `q` in the public key line.
- Added regression test to ensure `q` is treated as text input (not global quit) during key import input stages.

## [v0.0.13] - 2026-03-19

### Added
- `dpx update` now emits progress events and renders a terminal download progress bar while fetching release assets.

### Changed
- Windows can now run fullscreen Bubble Tea TUI when terminal is interactive (TTY), matching Linux/macOS experience.
- Added `DPX_TUI_MODE` override:
  - `fallback`/`plain` forces text fallback TUI
  - `fullscreen`/`bubble` keeps fullscreen TUI when TTY is available

### Fixed
- Fullscreen TUI import flow now handles key-block paste better:
  - detects accidental key-block paste in `From file` input
  - supports line-by-line key-block paste with `Ctrl+D` finalize
- Improved TUI navigation ergonomics with additional keys (`Tab`, `Shift+Tab`, `Ctrl+N`, `Ctrl+P`, `Home/End`, `Ctrl+B` back).

## [v0.0.12] - 2026-03-19

### Changed
- `dpx tui` now forces fallback TUI on Windows to avoid multiline key-paste instability in fullscreen mode.

### Fixed
- Import key flow in fallback TUI now tolerates accidental paste at the `From file` prompt:
  - if input looks like key-block content (`# ...` metadata or `AGE-SECRET-KEY-...`), it is parsed as key content instead of a filesystem path
  - multiline key block can continue immediately and is merged safely before import
- Added regression coverage for Windows fallback selection and pasted key-block import handling.

## [v0.0.11] - 2026-03-18

### Added
- `keygen` help/docs now document key import workflow:
  - `--import-file`
  - `--import-stdin`
  - `.dpx.yaml` auto-sync behavior
- New self-update commands:
  - `dpx update`
  - `dpx update --version vX.Y.Z`
  - `dpx rollback`
- Cross-platform update asset resolution for Linux/macOS/Windows with rollback backup support.

### Changed
- `dpx keygen --import-file <age-keys.txt>` now uses the import file path as default output when `--out` is omitted.
- TUI import flow now pre-fills the output key path with the selected import file path.

### Fixed
- TUI key import now handles pasted `age-keys.txt` blocks more safely:
  - waits for `AGE-SECRET-KEY-...` in Bubble Tea input before proceeding
  - fallback TUI auto-stops pasted block at the private-key line (no mandatory `END`)
  - clearer validation when no private key line is found
- Import parser now tolerates noisy shell paste artifacts and extracts the first valid `AGE-SECRET-KEY-...` token.

## [v0.0.9] - 2026-03-18

### Added
- Password envelope metadata now includes:
  - `Encryption-Algorithm`
  - `Encryption-Nonce`
- Password KDF profiles for brute-force resistance:
  - `balanced` (default CLI)
  - `hardened`
  - `paranoid`
- New CLI flags for password flows:
  - `dpx encrypt --kdf-profile ...`
  - `dpx env encrypt --kdf-profile ...`
  - `dpx env set --kdf-profile ...`
- TUI/fallback password encryption defaults to `hardened` KDF profile.

### Changed
- Password envelope encryption now binds metadata to ciphertext integrity using authenticated AAD.
- `dpx decrypt` now auto-detects inline `.env.dpx` (`ENC[...]`) and routes to inline decrypt flow.
- CLI help and docs updated for KDF profiles and strengthened security defaults.

### Fixed
- Improved decrypt UX for plaintext/non-envelope files with clearer error:
  - not a DPX envelope and no inline ENC tokens found.
- Maintained backward compatibility for legacy password envelope format (nonce-prefixed payload).

## [v0.0.8] - 2026-03-18

### Added
- `dpx run` command to load env values from `.env`, `.env.dpx`, or inline encrypted `.env.dpx` and inject them into a child process.
- `dpx policy check` command to detect plaintext sensitive keys in env/json/yaml-like files.
- `dpx env list` and `dpx env get` for reading env keys from plaintext/encrypted sources.
- `dpx env set` to add/update env keys with optional inline encryption.
- `dpx env updatekeys` to rotate recipients for inline `ENC[age:...]` values.
- `policy.creation_rules` support in `.dpx.yaml` for default mode and key selection per file pattern.
- New docs:
  - `docs/env-inline-workflow.md`
  - `docs/creation-rules.md`
  - `docs/testing-report-2026-03-18.md`

### Changed
- Expanded CLI help text and README examples for new env/runtime/policy commands.
- Added broader automated coverage for env inline workflows, recipient rotation, policy checks, and runtime injection.

## [v0.0.7] - 2026-03-17

### Added
- Inline `.env` token encryption/decryption support with:
  - `ENC[age:...]`
  - `ENC[pwd:v1:...]`
- `dpx env encrypt` and `dpx env decrypt` workflows for inline `.env` values.
- Interactive key selection for env-inline encryption.
- Password confirmation prompts on encrypt flows (CLI and TUI env-inline).
- Automated tests for env-inline service, CLI, and TUI round-trip flows.

### Changed
- Improved interactive CLI help output with guided usage examples.
- Expanded README to document interactive flow and env-inline commands.

## [v0.0.6] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.6>

## [v0.0.5] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.5>

## [v0.0.4] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.4>

## [v0.0.3] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.3>

## [v0.0.2] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.2>

## [v0.0.1] - 2026-03-17

See release notes: <https://github.com/dwirx/dpx/releases/tag/v0.0.1>
