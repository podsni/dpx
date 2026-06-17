# Installing `dpx`

`dpx` is distributed as a single static binary for Linux, macOS, and Windows
(both `amd64` and `arm64`). Releases are hosted at
[`podsni/dpx`](https://github.com/podsni/dpx/releases) (the personal fork).

## Quick install (recommended)

### Linux / macOS

```bash
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash
```

The installer detects your OS + arch, downloads the matching asset, and
installs to `/usr/local/bin` (if writable) or `~/.local/bin` (otherwise).
It then prints `dpx --version`.

### Windows (PowerShell 5+)

```powershell
iex (iwr https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.ps1).Content
```

Installs to `%LOCALAPPDATA%\dpx\bin` and prepends it to your user `PATH`.

## Pinned version

```bash
# Linux / macOS
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh \
  | bash -s -- --version v0.0.17

# Windows PowerShell
$env:DPX_VERSION = "v0.0.17"
iex (iwr https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.ps1).Content
```

## Custom install directory

```bash
curl -fsSL ... | bash -s -- --install-dir /opt/bin

# or via env
DPX_INSTALL_DIR=/opt/bin curl -fsSL ... | bash
```

## Manual install

1. Download the right asset from
   <https://github.com/podsni/dpx/releases/latest>:
   - `dpx_linux_amd64.tar.gz`
   - `dpx_linux_arm64.tar.gz`
   - `dpx_darwin_amd64.tar.gz`
   - `dpx_darwin_arm64.tar.gz`
   - `dpx_windows_amd64.zip`
   - `dpx_windows_arm64.zip`
2. Extract the archive.
3. Move `dpx` (or `dpx.exe` on Windows) to a directory on your `PATH`.
4. Verify: `dpx --version`.

## Build from source

```bash
git clone https://github.com/podsni/dpx.git
cd dpx
make build VERSION=dev           # produces ./dpx
make test                        # full test suite
```

Cross-compile (no Go toolchain required on the target):

```bash
# From a Linux build host
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o dpx-darwin-arm64  ./cmd/dpx
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=dev" -o dpx-windows-amd64.exe ./cmd/dpx
```

To produce the full release asset bundle (matches what CI uploads):

```bash
make release VERSION=vX.Y.Z
ls dist/
# dpx_darwin_amd64.tar.gz
# dpx_darwin_arm64.tar.gz
# dpx_linux_amd64.tar.gz
# dpx_linux_arm64.tar.gz
# dpx_windows_amd64.zip
# dpx_windows_arm64.zip
# install.sh
# install.ps1
# checksums.txt
```

## Verify

```bash
dpx --version    # dpx vX.Y.Z
dpx doctor       # reports config + key status
```

## Self-update

`dpx` ships with a built-in self-updater:

```bash
dpx update                # update to latest
dpx update --version v0.0.18   # update to a specific version
dpx rollback              # restore the previous binary (post-update rollback)
```

`dpx uninstall --yes [--remove-key] [--remove-encrypted]` is the inverse.

## Environment variables

| Variable | Used by | Purpose |
|----------|---------|---------|
| `DPX_VERSION` | `install.sh`, `install.ps1` | Pin to a specific release version |
| `DPX_INSTALL_DIR` | `install.sh`, `install.ps1` | Override install location |
| `DPX_INSTALL_BASE_URL` | `install.sh`, `install.ps1` | Override base URL (private mirror, fork, etc.) |
| `DPX_REPO` | `install.sh`, `install.ps1` | Override GitHub repo (e.g. `dwirx/dpx` for the upstream release) |
| `DPX_GITHUB_TOKEN` | `dpx dlx` | GitHub PAT for `dlx` rate-limit / auth-required repos |
| `DPX_TUI_MODE` | `dpx tui` | Force `fallback` / `plain` / `fullscreen` / `bubble` |

## Switching to the upstream build

If you ever need to install from `dwirx/dpx` (the upstream) instead of
`podsni/dpx` (this fork), override the repo:

```bash
DPX_REPO=dwirx/dpx curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash
# or
DPX_REPO=dwirx/dpx curl -fsSL https://raw.githubusercontent.com/dwirx/dpx/main/scripts/install.sh | bash
```

By default, the install scripts installed with this repo point at
`podsni/dpx`.

## Uninstall

```bash
dpx uninstall --yes                          # remove binary
dpx uninstall --yes --remove-key             # also remove local age key
dpx uninstall --yes --remove-encrypted       # also remove all .dpx files
```

The binary is removed from `DPX_INSTALL_DIR` (default: `/usr/local/bin` or
`~/.local/bin`).

## Verifying checksums

Each release includes `checksums.txt`:

```bash
curl -fsSL https://github.com/podsni/dpx/releases/latest/download/checksums.txt | \
  grep dpx_linux_amd64.tar.gz
# then compare with
sha256sum dpx_linux_amd64.tar.gz
```

## See also

- [`skill/SKILL.md`](../skill/SKILL.md) — agent-facing quick reference
- [`docs/dpx-dlx.md`](dpx-dlx.md) — `dpx dlx` usage
- [`docs/agent-skill-usage.md`](agent-skill-usage.md) — older agent notes
- [`CHANGELOG.md`](../CHANGELOG.md) — version history
