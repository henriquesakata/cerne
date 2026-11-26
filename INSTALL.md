# Installing cerne

cerne is a single executable with no dependencies. Put it on your `PATH` and you are done.

## Homebrew (macOS and Linux)

```bash
brew install henriquesakata/tap/cerne
```

`cerne update` updates it later (see [Updating](#updating)). Use the full name `henriquesakata/tap/cerne`: since Homebrew 6.0 a third-party tap must be trusted before use, and installing by the full name trusts this formula only, in the same command.

## Arch Linux (AUR)

> **Not published yet.** The AUR closed new account registrations before the
> `cerne-bin` package could be created, so it does not exist yet. It ships
> with the first stable release after registrations reopen. Until then, use
> the [quick install](#quick-install-macos-and-linux).

With an AUR helper:

```bash
yay -S cerne-bin
```

Without one:

```bash
git clone https://aur.archlinux.org/cerne-bin.git
cd cerne-bin
makepkg -si
```

The package installs the release binary as `/usr/bin/cerne` (x86_64 and aarch64), checked against the release's `SHA256SUMS`, and each stable release updates it. Update it with your helper (`yay -Syu cerne-bin`): `cerne update` leaves a package-managed binary to pacman (see [Updating](#updating)).

## Quick install (macOS and Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/henriquesakata/cerne/main/install.sh | sh
```

The script picks the release file for your machine, verifies it against the release's `SHA256SUMS` and installs it as `cerne` in `/usr/local/bin` when you can write there, otherwise in `~/.local/bin`. It never calls `sudo` and never edits your shell profile; if the directory is not on your `PATH`, it prints the line to add. To update later, run `cerne update` (see [Updating](#updating)).

| Variable | Effect |
|---|---|
| `CERNE_VERSION` | Install a specific release, e.g. `CERNE_VERSION=v0.5.0` |
| `CERNE_INSTALL_DIR` | Install into this directory instead |

To set one, put it before `sh`: `curl -fsSL … | CERNE_VERSION=v0.5.0 sh`.

## Quick install (Windows)

In PowerShell (Windows PowerShell 5.1 or PowerShell 7):

```powershell
irm https://raw.githubusercontent.com/henriquesakata/cerne/main/install.ps1 | iex
```

The script downloads `cerne.exe` for the latest release, verifies it against the release's `SHA256SUMS` and installs it in `%LOCALAPPDATA%\Programs\cerne`, without admin rights. It adds that folder to your user `PATH` (and to the current session) when it is not there yet, so `cerne` works right away and in new terminals. To update later, run `cerne update` (see [Updating](#updating)); a running `cerne ui` does not block it. On Windows on ARM it installs the amd64 build, which Windows 11 runs under emulation.

It reads the same `CERNE_VERSION` and `CERNE_INSTALL_DIR` variables, set before the line: `$env:CERNE_VERSION = 'v0.5.0'; irm … | iex`.

Then skip to [What you need](#3-what-you-need). To install by hand, follow sections 1 and 2.

## Scoop (Windows)

```powershell
scoop bucket add henriquesakata https://github.com/henriquesakata/scoop-bucket
scoop install henriquesakata/cerne
```

The manifest installs the release's `cerne.exe`, checked against its SHA-256, and each stable release updates it. Update it with `scoop update cerne`: `cerne update` leaves a Scoop install to Scoop (see [Updating](#updating)).

## Shell completion

`cerne completion <shell>` prints a script that completes cerne's commands and flags when you press Tab. Run the block for your shell once; it writes only inside your home folder, no `sudo`. Then open a new shell and try `cerne fea<Tab>`. To try it in the current shell only, without saving anything, use the one-liner from `cerne completion <shell> --help`.

**bash** needs the `bash-completion` package, version 2 (most Linux distributions install it; on macOS run `brew install bash-completion@2` and add the line it prints to `~/.bash_profile`):

```bash
mkdir -p ~/.local/share/bash-completion/completions
cerne completion bash > ~/.local/share/bash-completion/completions/cerne
```

**zsh** (if your `~/.zshrc` does not turn completion on yet, as oh-my-zsh and most setups do, first add `autoload -U compinit; compinit` to it):

```zsh
echo 'source <(cerne completion zsh)' >> ~/.zshrc
```

**fish:**

```fish
mkdir -p ~/.config/fish/completions
cerne completion fish > ~/.config/fish/completions/cerne.fish
```

**PowerShell:**

```powershell
if (!(Test-Path $PROFILE)) { New-Item -Path $PROFILE -ItemType File -Force | Out-Null }
Add-Content $PROFILE 'cerne completion powershell | Out-String | Invoke-Expression'
```

## Verifying a release

Every release file carries a build provenance attestation, signed by the GitHub Actions run that built it. With the [GitHub CLI](https://cli.github.com/):

```bash
gh attestation verify cerne_v0.5.0_darwin_arm64 --repo henriquesakata/cerne
```

It confirms the file was built by this repository's release workflow, from the tagged commit. The install scripts check the SHA-256 against `SHA256SUMS`; the attestation also proves where `SHA256SUMS` came from.

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

A binary installed this way from a release tag (`@latest`, `@v0.5.0`) reports that version, like the release binaries.

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

The [quick install](#quick-install-windows) does all of this in one line. By hand, in the folder that holds `cerne.exe`:

```powershell
Unblock-File .\cerne.exe
$dir = "$env:LOCALAPPDATA\Programs\cerne"
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item .\cerne.exe $dir -Force
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
```

Close and reopen the terminal, then run `cerne --version`. If Windows shows *"Windows protected your PC"*, click **More info → Run anyway**.

### Updating

Run `cerne update` inside your project. It updates the binary the way you installed it, then renews the hooks and the authoring guide of the agents already connected to that project:

- **Homebrew:** it runs `brew upgrade henriquesakata/tap/cerne`, so Homebrew's own records stay right.
- **Arch Linux (AUR):** it leaves `/usr/bin/cerne` alone, because pacman replaces it, and prints `update the cerne-bin package with your AUR helper (for example: yay -Syu cerne-bin)`.
- **Scoop:** it leaves the binary alone, because Scoop replaces it, and prints `scoop update cerne`.
- **Install script, or a release file put in place by hand:** it downloads the latest release, checks it against the release's `SHA256SUMS` and replaces the executable in the same place. If the check or the swap fails, the old executable stays in place and working.
- **A local build or a `go install`, or a folder you cannot write:** it leaves the binary alone and prints the command to run.

It ends with one line per part (binary, hooks, guide): updated, up to date, or not done with the reason and the command to run. The exit code is 0 only when nothing was left not done. New hooks apply from your agent's next session. `cerne update --check` only lists the project files that are out of date: it writes nothing and uses no network. Outside a project, `cerne update` updates the binary alone, so run it again inside each project. An agent can run it too, but only to renew the project files: replacing the binary is left to you, in your own terminal.

After a new binary the hooks and the guide of a connected agent renew by themselves, at your agent's session start and in `cerne next`, when every managed file of that agent is exactly one a past cerne release wrote: nothing you edited is overwritten, and a line says which agents were renewed (the change applies from the next session). When any file of an agent is one cerne does not recognize (you edited it, or a newer cerne wrote it), it touches nothing of that agent and only warns, naming the file; `cerne update` replaces it when you decide. It uses no network and connects no new agent. To turn it off, run `cerne settings set hooks.auto_refresh false` (add `--scope host` for every project on this machine).

On a version without the command (`cerne update` answers `unknown command`), update the binary first with `brew upgrade henriquesakata/tap/cerne` or the install script, then run `cerne update` in each project.

## 3. What you need

- **git**: recommended. Without it cerne still works, falling back to a built-in Git implementation that is much slower.
- **A git repository to be the *backend*** (optional: without one, `cerne init` creates a local one): this is where the documentation lives (JSON + markdown + event history). Create an empty private repo on GitHub/GitLab. Authentication:
  - **SSH** (`git@github.com:you/my-project-docs.git`): uses your SSH agent, or your key files (`~/.ssh/id_ed25519`, `id_rsa` or `id_ecdsa`). Check that `ssh -T git@github.com` works. For a passphrase-protected key, load it into the agent first with `ssh-add`.
  - **HTTPS** (`https://…`): export a token in `CERNE_GIT_TOKEN` (and the user in `CERNE_GIT_USER`, if the host requires it).
  - **Just trying it out?** Skip the backend: `cerne init --name my-project` creates a local one for you (see below).
- **An AI agent** (optional, recommended): cerne is built to work alongside one. `init` installs the skills and hooks that teach Claude Code to use cerne, and delivers the authoring guide to the other agents it detects: Cursor, Codex CLI, Gemini CLI and GitHub Copilot (see "AI agents" below). Claude Code is the one used extensively; Cursor has had light use, Codex CLI is in early testing, GitHub Copilot is verified on the Copilot CLI only (VS Code agent mode and the cloud agent are not verified, and it has had little real-project use), and Gemini CLI has not been tested yet.

## 4. Getting started

Inside your project's repository:

<!-- quickstart -->
```bash
cerne init --name my-project   # no --backend: creates a local backend in your user data dir
# or, for a team, an existing empty remote:
# cerne init --backend git@github.com:you/my-project-docs.git --name my-project
git add .cernerc docs.lock .gitattributes .gitignore
git commit -m "chore: adopt cerne"
```

cerne scaffolds your documents (specs, plans, results, decisions, runbooks) in English. For Portuguese, add `--docs-language pt-BR` to `cerne init`. The language is recorded in the backend, so everyone on the project gets the same one.

Besides those files, `init` also writes:

- `.claude/` for Claude Code: the `cerne-authoring` and `cerne-self-learning` skills, hooks, and `settings.json`. This repository commits `.claude/settings.json` and `.claude/hooks/` and ignores `.claude/skills/`, which `cerne skill install` recreates.
- Two git hooks in `.git/hooks/`: `prepare-commit-msg` and `commit-msg`. The latter refuses a commit that cites a feature id (`F-NNNN`) still in candidate or approved state, or already closed.

### AI agents

`init` and `cerne skill install` deliver the `cerne-authoring` guide to Claude Code and to every agent whose config folder exists in the project (`.cursor/`, `.codex/`, `.gemini/`; for Copilot, one of `.github/copilot-instructions.md`, `.github/hooks/`, `.github/skills/` or `.github/agents/`, never `.github/` alone), and print one line per agent, including the ones not detected. "Delivered" means the file is in place: cerne cannot verify that an agent loads it, so each line adds what the agent's own docs say about loading. `cerne hook install` wires the hooks the same way (Claude Code plus detected agents). To add an agent whose folder is missing, create it with its hooks first, then deliver the guide:

```bash
cerne hook install --provider cursor,codex,gemini,copilot   # hooks; creates each agent's config folder
cerne skill install                                 # the authoring guide, for agents whose config folder exists
```

| Agent | Hooks | Authoring guide (`cerne-authoring`) | Load condition (agent's docs) |
|---|---|---|---|
| Claude Code | `.claude/hooks/`, `.claude/settings.json` | `.claude/skills/` | loaded automatically |
| Cursor | `.cursor/rules/cerne.mdc`, `.cursor/hooks.json` | `.cursor/skills/` | documented as loaded automatically; no setting documented |
| Codex CLI | `.codex/hooks.json`, `.codex/config.toml` | `.agents/skills/`, plus a one-line pointer in `AGENTS.md` | detected automatically; restart Codex if it does not appear |
| Gemini CLI | `.gemini/settings.json`, `.gemini/.env` | `.gemini/skills/`, plus a one-line pointer in `GEMINI.md` | only while the skill is enabled (`/skills list`, `/skills enable cerne-authoring`); asks your consent on activation |
| GitHub Copilot | `.github/hooks/cerne.json`, `.mcp.json` | `.github/skills/`, plus `.github/instructions/cerne.instructions.md` | see "Using cerne with GitHub Copilot" below |

Some hooks cannot be hosted by a given agent's extension surface; the hook install prints them as `skipped`, with the manual step. The full table is in [docs/provider-parity.md](docs/provider-parity.md).

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

### Using cerne with GitHub Copilot

There are two separate things, and you can use either one without the other: Copilot as the agent that works in your project, and the Copilot CLI as the model cerne itself calls.

**As the project's agent.** `init` detects Copilot only when `.github/` already holds `copilot-instructions.md`, `hooks/`, `skills/` or `agents/`, so ask for it explicitly:

```bash
cerne hook install --provider copilot   # hooks and MCP
cerne skill install                     # the authoring guide and instructions
```

What lands in the project:

| File | What it is |
|---|---|
| `.github/hooks/cerne.json` | The hooks: the work gate (an edit is refused until you have started a unit of work), session context, provenance. Wholly owned by cerne; a teammate's other files in `.github/hooks/` are untouched |
| `.github/skills/cerne-authoring/` | The authoring guide, with its references |
| `.github/instructions/cerne.instructions.md` | A short instructions file that applies to every file (`applyTo: "**"`); `.github/copilot-instructions.md` is never touched |
| `.mcp.json` | The read-only `cerne mcp` server, merged into your existing file without disturbing other servers |

Where it was verified: the hooks, the skill, the instructions and the MCP server were exercised on the **Copilot CLI** (1.0.112). **VS Code agent mode** and the **Copilot cloud agent** were not verified. The cloud agent does not read `.mcp.json`: its MCP servers are set in the repository's settings on GitHub, and `cerne hook install` prints the block to paste there. Some hooks cannot be hosted by Copilot's extension surface (for example the subagent and compaction ones); the install prints them as `skipped`, and [docs/provider-parity.md](docs/provider-parity.md) lists each one.

**As cerne's AI provider.** Commands that call a model (`cerne ask`, the draft commands, the dashboard's chat) can use your Copilot subscription through the Copilot CLI, with no API key:

```toml
# .cernerc
[llm]
mode = "copilot-cli"
```

Install the Copilot CLI and sign in once with `copilot login`. cerne runs it with every tool, MCP server and instruction file turned off, in an empty temporary folder and a throwaway `COPILOT_HOME`, so it only answers the prompt. On macOS the login lives in the keychain and carries over. On Linux and Windows the stored login may not be visible to that throwaway home: export `COPILOT_GITHUB_TOKEN` (a token with Copilot access) instead. By default Copilot picks the model (`auto`); set `llm.model` only to a model your subscription actually offers, because the CLI cannot list them and refuses one it does not offer.
