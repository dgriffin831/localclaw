# Installation Guide

This guide installs `localclaw` from source and makes the `localclaw` command available on `PATH` for:

- Windows
- macOS (Apple Silicon / arm64)
- Linux (x86_64 and arm64)

It is written to be copy/paste friendly for both humans and coding agents.

## Prerequisites

- `git`
- Go `1.24.2` or newer (`go version`)

Repository URL:

```text
https://github.com/dgriffin831/localclaw.git
```

## Windows (PowerShell)

Run in **PowerShell**:

```powershell
# 1) Install Git + Go (requires winget)
winget install -e --id Git.Git
winget install -e --id GoLang.Go

# 2) Clone repo
git clone https://github.com/dgriffin831/localclaw.git
Set-Location localclaw

# 3) Build binary into user bin dir
New-Item -ItemType Directory -Force "$HOME\bin" | Out-Null
go build -o "$HOME\bin\localclaw.exe" .\cmd\localclaw

# 4) Add user bin dir to PATH (idempotent)
$userBin = "$HOME\bin"
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not (($userPath -split ';') -contains $userBin)) {
  [Environment]::SetEnvironmentVariable("Path", "$userBin;$userPath", "User")
}
$env:Path = "$userBin;$env:Path"

# 5) Verify
go version
Get-Command localclaw
localclaw doctor
```

If `winget` is unavailable, install Git from [git-scm.com](https://git-scm.com/downloads/win) and Go from [go.dev/dl](https://go.dev/dl/), then run steps 2-5.

## macOS (Apple Silicon / arm64)

Run in **Terminal** (`zsh` or `bash`):

```bash
# 1) Install Git tools (if needed)
xcode-select --install 2>/dev/null || true

# 2) Install Go 1.24.2 (arm64)
curl -LO https://go.dev/dl/go1.24.2.darwin-arm64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.24.2.darwin-arm64.tar.gz
rm go1.24.2.darwin-arm64.tar.gz

# 3) Clone repo
git clone https://github.com/dgriffin831/localclaw.git
cd localclaw

# 4) Build binary into user bin dir
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/localclaw" ./cmd/localclaw

# 5) Add Go + local bin to PATH (zsh)
PATH_LINE='export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"'
grep -qxF "$PATH_LINE" "$HOME/.zshrc" || echo "$PATH_LINE" >> "$HOME/.zshrc"
export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"

# 6) Verify
go version
command -v localclaw
localclaw doctor
```

If you use `bash`, replace `~/.zshrc` with `~/.bashrc`.

## Linux (x86_64 or arm64)

Run in **bash**:

```bash
# 1) Install Git + curl + tar (examples)
# Debian/Ubuntu:
# sudo apt-get update && sudo apt-get install -y git curl tar
# Fedora:
# sudo dnf install -y git curl tar
# Arch:
# sudo pacman -Sy --noconfirm git curl tar

# 2) Install Go 1.24.2 (select arch)
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) GO_ARCH="amd64" ;;
  aarch64|arm64) GO_ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

curl -LO "https://go.dev/dl/go1.24.2.linux-${GO_ARCH}.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go1.24.2.linux-${GO_ARCH}.tar.gz"
rm "go1.24.2.linux-${GO_ARCH}.tar.gz"

# 3) Clone repo
git clone https://github.com/dgriffin831/localclaw.git
cd localclaw

# 4) Build binary into user bin dir
mkdir -p "$HOME/.local/bin"
go build -o "$HOME/.local/bin/localclaw" ./cmd/localclaw

# 5) Add Go + local bin to PATH
PATH_LINE='export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"'
grep -qxF "$PATH_LINE" "$HOME/.bashrc" || echo "$PATH_LINE" >> "$HOME/.bashrc"
export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"

# 6) Verify
go version
command -v localclaw
localclaw doctor
```

If you use `zsh`, add the same `PATH_LINE` to `~/.zshrc`.

## Agent/CLI Integration Check

After install, coding agents (for example Claude Code or Codex CLI) should be able to run:

```bash
localclaw doctor
```

Notes:

- `doctor` performs runtime/path checks without sending a model prompt.
- `doctor --deep` also runs an LLM prompt probe and requires your configured provider CLI to be installed/configured.

If an agent still cannot find `localclaw`:

1. Restart the terminal/session where the agent runs.
2. Confirm `PATH` includes the binary location:
   - Windows: `%USERPROFILE%\bin`
   - macOS/Linux: `$HOME/.local/bin`
3. Confirm command resolution:
   - Windows: `Get-Command localclaw`
   - macOS/Linux: `command -v localclaw`
4. If `localclaw doctor` fails with `config error: parse config: json: unknown field ...`, you likely have an older `~/.localclaw/localclaw.json`. Update/remove that file or run with `-config /path/to/current-config.json`.
