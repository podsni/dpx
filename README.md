# DPX

`dpx` is a Go CLI and TUI tool for encrypting `.env` and similar local secret files into `.dpx` envelopes.

It supports two practical encryption modes:
- `age` for public-key workflows and team sharing
- `Argon2id + XChaCha20-Poly1305` for password-based encryption

It also ships with `dpx dlx` for downloading GitHub files/folders (or any HTTPS URL) without cloning the whole repo.

> **Fork notice**: This repository is the personal fork at
> [`podsni/dpx`](https://github.com/podsni/dpx). The upstream lives at
> [`dwirx/dpx`](https://github.com/dwirx/dpx); all upstream history is
> preserved and `v0.0.17+` adds `dpx dlx`, friendly CLI errors, and a
> smarter `dpx doctor`. See [`CHANGELOG.md`](./CHANGELOG.md) for the
> delta.

Latest changes are tracked in [`CHANGELOG.md`](./CHANGELOG.md).

Additional guides:
- [`docs/env-inline-workflow.md`](./docs/env-inline-workflow.md)
- [`docs/creation-rules.md`](./docs/creation-rules.md)
- [`docs/password-workflow.md`](./docs/password-workflow.md)
- [`docs/installation.md`](./docs/installation.md)
- [`docs/dpx-dlx.md`](./docs/dpx-dlx.md)
- [`docs/agent-skill-usage.md`](./docs/agent-skill-usage.md)
- [`docs/testing-report-2026-03-18.md`](./docs/testing-report-2026-03-18.md)
- [`docs/testing-report-2026-03-20.md`](./docs/testing-report-2026-03-20.md)

## ✨ Features

- Encrypt `.env` into `.env.dpx`
- Decrypt back to the original filename by default
- Guided CLI and interactive TUI modes
- Smart file suggestions for `.env`, `.env.*`, `*.env`, `.secret*`, `.credentials*`
- Inline `.env` key encryption with mode-blind token: `API_KEY=ENC[v2:...]`
- Backward compatible inline decrypt for legacy tokens (`ENC[age:...]`, `ENC[pwd:v1:...]`)
- Password confirmation on encrypt flows (CLI + TUI) to reduce typo risk
- Password KDF profiles: `balanced`, `hardened`, `paranoid` (`--kdf-profile`)
- Password generator command: `dpx genpass` (`--length`, `--copy-password`)
- Password rotation command: `dpx repassword` for `.dpx` + inline password tokens
- `dpx dlx` — download GitHub files/folders or any HTTPS URL (`--output`, `--no-prefix`, `--ref`, `--glob`, `--max-size`, `--token`)
- Friendly CLI error messages with actionable hints (`dpx e none.txt` → "source file not found: ... Check the path, or use 'dpx doctor'")
- Smarter `dpx doctor` with context-aware remediation suggestions
- Safe `uninstall` command with confirmation and cleanup flags
- `rotate` command to regenerate age keypairs and re-encrypt everything
- `hook install` / `uninstall` git pre-commit hooks for `policy check`
- `run` command to load env then exec child process
- `policy check` for plaintext-sensitive keys
- Hidden password prompt on real terminals
- Armored `.dpx` file format with metadata tamper detection
- GitHub Actions CI (Ubuntu + macOS + Windows) and manual release workflow
- Quick install scripts for Linux, macOS, and Windows
- Self-update + rollback: `dpx update`, `dpx rollback`

## 🚀 Quick Install

Recommended install from this fork's GitHub Releases.

### Linux and macOS

```bash
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash
```

To pin a version:

```bash
curl -fsSL https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.sh | bash -s -- --version v0.0.17
```

### Windows PowerShell

```powershell
iex (iwr https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.ps1).Content
```

To pin a version:

```powershell
$env:DPX_VERSION = "v0.0.17"
iex (iwr https://raw.githubusercontent.com/podsni/dpx/main/scripts/install.ps1).Content
```

After install:

```bash
dpx --version
dpx doctor
```

See [`docs/installation.md`](./docs/installation.md) for manual install, cross-platform notes, and self-update.

## 📦 Download Binary

Download from GitHub Releases:
<https://github.com/podsni/dpx/releases/latest>

| Platform | Architecture | Download |
| --- | --- | --- |
| Linux | x64 | `dpx_linux_amd64.tar.gz` |
| Linux | ARM64 | `dpx_linux_arm64.tar.gz` |
| macOS | Intel | `dpx_darwin_amd64.tar.gz` |
| macOS | Apple Silicon | `dpx_darwin_arm64.tar.gz` |
| Windows | x64 | `dpx_windows_amd64.zip` |
| Windows | ARM64 | `dpx_windows_arm64.zip` |

### Manual Install

Linux and macOS:

```bash
tar -xzf dpx_*.tar.gz
sudo mv dpx /usr/local/bin/
dpx --version
```

Windows:
- Extract the `.zip`
- Move `dpx.exe` to a folder in your `PATH`
- Run `dpx --version`

## 🧰 Install via Go

```bash
go install github.com/podsni/dpx/cmd/dpx@v0.0.17
```

Or `@latest` for the newest tagged release.

## 🛠️ Build From Source

```bash
git clone https://github.com/podsni/dpx
cd dpx
make build VERSION=dev
sudo mv dpx /usr/local/bin/  # Linux/macOS
```

## ⚡ Quick Start

### 1. Initialize a project

```bash
dpx init
```

Expected output:

```text
✅ Created .dpx.yaml

Next steps:
  1. Run 'dpx keygen' to generate a key pair
  2. Add your public key to .dpx.yaml
  3. Run 'dpx encrypt <file>' to encrypt your secrets
```

### 2. Generate an `age` key pair

```bash
dpx keygen
```

Example output:

```text
╔══════════════════════════════════════════════════════════════════╗
║                  🔑 DPX Key Generated Successfully               ║
╠══════════════════════════════════════════════════════════════════╣
║ Backend: age                                                     ║
║ Key file: ~/.config/dpx/age-keys.txt                            ║
╠══════════════════════════════════════════════════════════════════╣
║ Public Key (add to .dpx.yaml):                                   ║
║   age1...                                                        ║
╚══════════════════════════════════════════════════════════════════╝
```

### 3. Encrypt with a password

```bash
dpx encrypt .env --password 'super-secret-password'
```

Output:

```text
Encrypted .env -> .env.dpx
```

### 4. Decrypt back

```bash
dpx decrypt .env.dpx --password 'super-secret-password'
```

Output:

```text
Decrypted .env.dpx -> .env
```

### 5. Use interactive CLI (guided)

```bash
dpx encrypt
```

Typical guided flow:
- pick a file from suggestions or manual path
- choose mode (`age` or `password`)
- if password mode, type password + confirm password
- confirm output path

### 6. Use the TUI

```bash
dpx tui
```

The TUI can:
- choose `Encrypt`, `Decrypt`, `Inspect`, `Generate Password`, `Repassword (Manual)`, `Repassword (Generate)`, `Env Inline Encrypt`, `Env Inline Decrypt`, `Env Set`, `Env Update Keys`, and `Policy Check`
- suggest likely secret files
- choose `Password` or `Age`
- prompt for recipients or password (+ confirmation when encrypting)
- apply `hardened` KDF profile by default for password-based encryption flows
- confirm output path
- import age key from file or paste key-block (supports line-by-line paste + `Ctrl+D` finalize)

Windows note:
- DPX uses fullscreen TUI on Windows when stdin/stdout are TTY.
- For troubleshooting, force fallback text TUI with `DPX_TUI_MODE=fallback`.
- If needed, force fullscreen mode with `DPX_TUI_MODE=fullscreen`.

## 🧭 Common Usage

### Encrypt with a password

Prompt for password interactively:

```bash
dpx encrypt .env
```

DPX will ask:
- `Password:`
- `Confirm password:`

Pass the password explicitly:

```bash
dpx encrypt .env --password 'secret' --kdf-profile hardened
```

Write to a custom output:

```bash
dpx encrypt .env --password 'secret' --out configs/prod.env.dpx
```

### Encrypt with `age`

Use recipients from `.dpx.yaml`:

```bash
dpx encrypt .env --age
```

Pass recipients directly:

```bash
dpx encrypt .env --age --recipient age1abc...,age1def...
```

### Decrypt a file

Password mode with prompt:

```bash
dpx decrypt .env.dpx
```

Password mode with explicit password:

```bash
dpx decrypt .env.dpx --password 'secret'
```

`age` mode with explicit identity file:

```bash
dpx decrypt .env.dpx --identity ~/.config/dpx/age-keys.txt
```

Restore to a different path:

```bash
dpx decrypt .env.dpx --out .env.restored
```

### Run app with decrypted env (local/CI)

Run a command with env injected from `.env` / `.env.dpx`:

```bash
dpx run -- node app.js
```

Specify encrypted source explicitly:

```bash
dpx run .env.dpx --password 'secret' -- node app.js
```

Use custom identity for `age` mode:

```bash
dpx run .env.dpx --identity ~/.config/dpx/age-keys.txt -- ./bin/server
```

### Update or rollback CLI binary

If your binary is named `podx`, replace `dpx` with `podx` in commands below.

Update to latest release:

```bash
dpx update
```

DPX shows a download progress bar while fetching release assets.

Update to a specific version:

```bash
dpx update --version v1.2.3
```

Rollback to the previous local backup:

```bash
dpx rollback
```

### Inline `.env` key encryption

Encrypt selected keys only (values become `ENC[...]` inline):

```bash
dpx env encrypt .env --mode password --keys API_KEY,JWT_SECRET
```

Decrypt inline encrypted keys:

```bash
dpx env decrypt .env.dpx
```

List keys from env source (plaintext/encrypted):

```bash
dpx env list .env.dpx --password 'secret'
```

Read a single key:

```bash
dpx env get .env.dpx --key API_KEY --password 'secret'
```

Set or update one key directly (plaintext or encrypted):

```bash
dpx env set .env --key API_KEY --value 'new-value'
dpx env set .env.dpx --key API_KEY --value 'new-value' --encrypt --mode age
```

Rotate `age` recipients for existing inline encrypted keys:

```bash
dpx env updatekeys .env.dpx --recipient age1new...,age1team...
```

Interactive inline flow:

```bash
dpx env encrypt
```

DPX can prompt for:
- `.env` file selection
- mode (`age` or `password`)
- key selection (`all` or specific indexes)
- password + confirm password (for password mode)

### Inspect metadata safely

```bash
dpx inspect .env.dpx
```

Example output:

```text
Version: 1
Mode: password
Original Name: .env
Created At: 2026-03-17 10:11:12+00:00
KDF: argon2id
```

### Check project readiness

```bash
dpx doctor
```

### Policy check (plaintext secret detection)

```bash
dpx policy check .env
```

This command helps detect sensitive keys that still appear in plaintext.

`doctor` reports:
- which config file is in use
- whether a legacy config is being used
- whether the key file exists
- number of configured recipients
- number of suggested files
- number of `.dpx` files in the current directory

### Git pre-commit hook (prevent leaks)

Install a local git hook that automatically blocks commits containing plaintext secrets:

```bash
dpx hook install
```

When installed, every `git commit` checks staged `.env` files using `dpx policy check`. If unencrypted sensitive keys are found, the commit is aborted instantly.

To remove it:

```bash
dpx hook uninstall
```

### Key rotation (regenerate keys)

Automatically generate a new age keypair and re-encrypt all `.dpx` files and inline secrets in your project:

```bash
dpx rotate
```

DPX will ask for stark confirmation before decrypting and re-encrypting your secrets. Your old private key will be safely backed up to `.bak` and `.dpx.yaml` will be auto-updated.

### Uninstall and cleanup

Preview in help:

```bash
dpx --help
```

Remove project config only (asks confirmation):

```bash
dpx uninstall
```

Full cleanup without prompt:

```bash
dpx uninstall --yes --remove-key --remove-encrypted
```

## 📚 CLI Reference

### `dpx init`

Create `.dpx.yaml` in the current directory.

Behavior:
- fails if `.dpx.yaml` already exists
- also fails if legacy `.dopx.yaml` already exists

### `dpx keygen [--out <path>] [--regen] [--import-file <age-keys.txt>] [--import-stdin] [--no-config-update]`

Generate an `age` identity file.

Default key path:

```text
~/.config/dpx/age-keys.txt
```

Import existing `age` key file format (`age-keys.txt`):

```text
# created: 2026-03-17T18:14:43Z
# public key: age1...
AGE-SECRET-KEY-...
```

Rules:
- `--import-file <path>` imports an existing key file and auto-syncs `.dpx.yaml`
- if `--import-file` is used without `--out`, DPX uses the import path as `key_file`
- imported public key is added to `age.recipients` automatically (unless already present)
- `--import-stdin` reads the same format from stdin
- `--no-config-update` skips `.dpx.yaml` sync

Cross-shell import examples:

PowerShell:

```powershell
dpx keygen --import-file .\age-keys.txt
# or
Get-Content -Raw .\age-keys.txt | dpx keygen --import-stdin
```

CMD:

```bat
dpx keygen --import-file age-keys.txt
type age-keys.txt | dpx keygen --import-stdin
```

Git Bash:

```bash
dpx keygen --import-file ./age-keys.txt
cat ./age-keys.txt | dpx keygen --import-stdin
```

Tip:
- avoid pasting wrapped key text directly in shell commands; save to `age-keys.txt` then use `--import-file`.

### `dpx uninstall [--yes] [--remove-key] [--remove-encrypted]`

Remove DPX files safely.

Behavior:
- removes `.dpx.yaml`/`.dopx.yaml` in current directory
- `--remove-key` removes key file only if it is in a safe scope (default/legacy path or inside current project)
- `--remove-encrypted` removes `.dpx` files in current directory
- without `--yes`, command asks for explicit confirmation (`YES`)

### `dpx update [--version <vX.Y.Z>]`

Self-update DPX binary from GitHub releases.

Rules:
- default pulls latest release asset for current OS/architecture
- `--version` pins to a specific release tag
- DPX stores previous binary as rollback backup (`.rollback`)
- optional env `DPX_UPDATE_BASE_URL` overrides asset base URL (advanced/testing)

### `dpx rollback`

Restore the previous local backup binary created by `dpx update`.

Rules:
- requires existing rollback backup
- on Windows update/rollback may be scheduled and applied after process exits

### `dpx encrypt <file> [--password <text>] [--age] [--recipient <csv>] [--kdf-profile <balanced|hardened|paranoid>] [--out <path>]`

Encrypt a file into `.dpx`.

Rules:
- `--password` selects password mode
- `--age` selects `age` mode
- if no output is provided, output becomes `<file>.dpx`
- if no file is provided, DPX starts guided picker/search flow
- if password is prompted interactively, DPX asks password confirmation

### `dpx decrypt <file.dpx> [--password <text>] [--identity <path>] [--out <path>]`

Decrypt a `.dpx` file.

Rules:
- DPX auto-detects password or `age` mode from metadata
- if no output is provided, DPX restores the original filename from metadata
- if password mode is detected and no password is provided, DPX prompts for it

### `dpx env encrypt [<file>] [--mode age|password] [--keys <csv>] [--recipient <csv>] [--password <text>] [--kdf-profile <balanced|hardened|paranoid>] [--out <path>]`

Encrypt selected `.env` keys inline into `ENC[...]` values.

Rules:
- when `<file>` is omitted, DPX suggests `.env` candidates
- when `--keys` is omitted, DPX asks interactive key selection
- in interactive password mode, DPX asks password confirmation

### `dpx env decrypt [<file.dpx>] [--password <text>] [--identity <path>] [--out <path>]`

Decrypt inline encrypted `ENC[...]` values back into plaintext env values.

### `dpx env set [<file>] --key <KEY> --value <VALUE> [--encrypt] [--mode age|password] [--recipient <csv>] [--password <text>] [--kdf-profile <balanced|hardened|paranoid>] [--out <path>]`

Set or replace one env key.

Rules:
- default behavior writes plaintext value
- use `--encrypt` to store as `ENC[...]` token
- if mode is omitted while encrypting, DPX picks from creation rule or config defaults

### `dpx env updatekeys [<file>] --recipient <csv> [--keys <csv>] [--identity <path>] [--out <path>]`

Re-encrypt existing inline age-backed keys (`ENC[v2:...]`) for new recipients.

Rules:
- requires the old private key (`--identity`) to decrypt current tokens
- `--keys` limits rotation to selected env keys
- if omitted, all inline age-encrypted keys are rotated

### `dpx inspect <file.dpx>`

Show safe metadata only.

### `dpx rotate [--yes]`

Automatically regenerate the age keypair, decrypt all current `.dpx` files and inline secrets, re-encrypt them with the new key, and update configurations. Using `--yes` bypasses the interactive destructive warning prompt.

### `dpx doctor`

Show project and local environment readiness.

### `dpx tui`

Launch the interactive interface.

### `dpx hook [install|uninstall]`

Manages a local git `pre-commit` hook. The hook runs `dpx policy check` prior to any commit, rejecting the commit if sensitive plaintext keys are detected in common `.env` files.

### `dpx version`
### `dpx --version`
### `dpx -v`

Print the current version.

## Config File

Primary config file:

```text
.dpx.yaml
```

Legacy config still supported:

```text
.dopx.yaml
```

Example:

```yaml
version: 1
default_suffix: ".dpx"
key_file: "~/.config/dpx/age-keys.txt"
age:
  recipients:
    - age1examplepublickey
discovery:
  include:
    - ".env"
    - ".env.*"
    - "*.env"
    - ".secret*"
    - ".credentials*"
policy:
  creation_rules:
    - path: ".env.production"
      mode: "age"
      encrypt_keys:
        - "API_KEY"
        - "JWT_SECRET"
```

## File Format

DPX writes armored text envelopes.

Current header prefix:

```text
DPX-File-Version: 1
```

Legacy envelopes are still accepted:

```text
DOPX-File-Version: 1
```

Default encrypted output:

```text
.env.dpx
```

Password-mode headers now include:

```text
Encryption-Algorithm: xchacha20poly1305
Encryption-Nonce: <base64>
```

## Security Notes

- Password mode uses `Argon2id`
- Password encryption uses `XChaCha20-Poly1305`
- Key mode uses `filippo.io/age`
- Password-mode metadata is bound to ciphertext integrity (AAD), so header tampering is rejected
- Use stronger KDF profiles (`hardened` / `paranoid`) for higher brute-force cost
- Outer `.dpx` metadata is checked against protected inner metadata
- Tampered metadata causes decryption to fail
- Decryption restores original bytes
- Password prompts are hidden on real terminals
- Keep private keys outside the repository

## GitHub Actions

This repository includes:
- CI workflow: `.github/workflows/ci.yml`
- Manual release workflow: `.github/workflows/release.yml`

### CI workflow

Runs on:
- push to `main`
- pull requests

Checks:
- `go test ./...`
- binary build
- release asset build
- Linux installer smoke test against locally served release assets

### Manual release workflow

The release workflow is triggered manually from GitHub Actions with a version input.

Example version input:

```text
v0.2.0
```

What the workflow does:
- validates the version string
- checks that the tag does not already exist
- runs tests
- builds release assets
- smoke tests `install.sh`
- creates and pushes the tag
- publishes a GitHub Release

## Release Assets

The release workflow publishes stable asset names:

- `dpx_linux_amd64.tar.gz`
- `dpx_linux_arm64.tar.gz`
- `dpx_darwin_amd64.tar.gz`
- `dpx_darwin_arm64.tar.gz`
- `dpx_windows_amd64.zip`
- `dpx_windows_arm64.zip`
- `install.sh`
- `install.ps1`
- `checksums.txt`

## Development

Run tests:

```bash
make test
```

Build:

```bash
make build VERSION=dev
```

Build local release assets:

```bash
make release VERSION=v0.2.0
```

Artifacts are written to `dist/`.

## Migration From `dopx`

DPX is the new primary name.

Compatibility retained:
- `.dpx.yaml` is preferred
- `.dopx.yaml` is still read
- `~/.config/dpx/age-keys.txt` is preferred
- `~/.config/dopx/age-keys.txt` is still used as fallback
- envelopes using `DOPX-File-Version` still decrypt

## Troubleshooting

### `config already exists`

You already have `.dpx.yaml` or `.dopx.yaml` in the working directory.

### `no candidate files found`

DPX did not find a file matching its discovery patterns in the current directory.

### Password decryption fails

Possible causes:
- wrong password
- tampered file
- corrupted payload

### `age` decryption fails

Possible causes:
- wrong identity file
- wrong recipient
- missing private key file

## Repository Hygiene

Do not commit:
- plaintext `.env` files with real secrets
- generated `.dpx` secret files
- private keys
- local binaries
- local `dist/` artifacts
