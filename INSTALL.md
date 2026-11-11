# Installing cerne

cerne is a single executable with no dependencies. Download the file for your system, put it on your `PATH`, and you are done.

## 1. Which file to download

Binaries are published on the [Releases page](https://github.com/henriquesakata/cerne/releases), with the version in the name (e.g. `cerne_v0.5.0_darwin_arm64`), next to a `SHA256SUMS` file.

| System | File |
|---|---|
| Mac with Apple silicon (M1, M2, M3…) | `darwin_arm64` |
| Mac with Intel | `darwin_amd64` |
| Windows | `windows_amd64.exe` |
| Linux (regular PC/server) | `linux_amd64` |
| Linux ARM (Raspberry Pi, Graviton…) | `linux_arm64` |

Not sure on Mac/Linux? Run `uname -m`: `arm64`/`aarch64` → arm64; `x86_64` → amd64.

Rename the downloaded file to `cerne` (or `cerne.exe` on Windows).

### With Go instead

If you have Go installed, you can build it yourself:

```bash
go install github.com/henriquesakata/cerne/cmd/cerne@latest
```

A binary built this way reports its version as `dev`; release binaries carry the tag.

## 2. Install

### macOS

In the terminal, in the folder that holds the file:

```bash
xattr -c cerne          # remove the download quarantine (otherwise macOS blocks it)
chmod +x cerne
sudo mkdir -p /usr/local/bin
sudo mv cerne /usr/local/bin/
cerne --version
```

If you see *"cerne cannot be opened because Apple cannot check it"*, the `xattr -c` was skipped: run `sudo xattr -c /usr/local/bin/cerne`.

### Linux

```bash
chmod +x cerne
sudo mv cerne /usr/local/bin/
cerne --version
```

### Windows (PowerShell)

In the folder that holds `cerne.exe`:

```powershell
Unblock-File .\cerne.exe
$dir = "$env:LOCALAPPDATA\Programs\cerne"
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item .\cerne.exe $dir -Force
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
```

Close and reopen the terminal, then run `cerne --version`. If Windows shows *"Windows protected your PC"*, click **More info → Run anyway**.

### Updating

Same process: replace the old executable with the new one.

## 3. What you need

- **git**: recommended. Without it cerne still works, falling back to a built-in Git implementation that is much slower.
- **An empty git repository to be the *backend***: this is where the documentation lives (JSON + markdown + event history). Create an empty private repo on GitHub/GitLab. Authentication:
  - **SSH** (`git@github.com:you/my-project-docs.git`): uses your SSH agent, or your key files (`~/.ssh/id_ed25519`, `id_rsa` or `id_ecdsa`). Check that `ssh -T git@github.com` works. For a passphrase-protected key, load it into the agent first with `ssh-add`.
  - **HTTPS** (`https://…`): export a token in `CERNE_GIT_TOKEN` (and the user in `CERNE_GIT_USER`, if the host requires it).
  - **Just trying it out?** A local folder works: `git init --bare ~/my-project-docs.git`.
- **Claude Code** (optional, recommended): cerne is built to work alongside an AI agent. `init` installs the skills and hooks that teach the agent to use cerne.

## 4. Getting started

Inside your project's repository:

```bash
cerne init --backend git@github.com:you/my-project-docs.git --name my-project
echo ".docs/" >> .gitignore
git add .cernerc docs.lock .gitattributes .gitignore
git commit -m "chore: adopt cerne"
```

Besides those files, `init` also writes:

- `.claude/` for Claude Code: the `cerne-authoring` and `cerne-self-learning` skills, hooks, and `settings.json`. This repository commits `.claude/settings.json` and `.claude/hooks/` and ignores `.claude/skills/`, which `cerne skill install` recreates.
- Two git hooks in `.git/hooks/`: `prepare-commit-msg` and `commit-msg`. The latter refuses a commit that cites a feature id (`F-NNNN`) still in candidate or approved state, or already closed.

Day to day:

```bash
cerne feature new "Sign in with Google" --context "Users asked for social login."
cerne pull          # materializes the current state into .docs/ (what the agent reads)
# edit .docs/current/features/F-0001-.../spec.md
cerne push          # sends your edits back to the backend
cerne status        # features by state (candidate → approved → in development → implemented → done)
cerne ui            # web dashboard at http://localhost:7717
```

State transitions are gated. `cerne feature approve` needs a real spec, including its `Barra:` line (the bar the delivery is measured against); `start` needs a real plan; `complete` needs a real result. `approve` and `accept` (implemented → done) are human acts by default. With Claude Code, ask the agent: the `cerne-authoring` skill installed by `init` drives the whole flow. All commands: `cerne --help`.
