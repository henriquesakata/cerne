# Instalar o cerne

O cerne é um único executável, sem dependências. Baixe o arquivo do seu sistema, coloque no `PATH` e pronto.

## 1. Qual arquivo baixar

| Sistema | Arquivo |
|---|---|
| Mac com chip Apple (M1, M2, M3…) | `darwin_arm64` |
| Mac com Intel | `darwin_amd64` |
| Windows | `windows_amd64` |
| Linux (PC/servidor comum) | `linux_amd64` |
| Linux ARM (Raspberry Pi, Graviton…) | `linux_arm64` |

Na dúvida no Mac/Linux, rode `uname -m` no terminal: `arm64`/`aarch64` → arm64; `x86_64` → amd64.

Se recebeu um `.zip`, ele traz o executável já com o nome `cerne` (ou `cerne.exe`). Se baixou direto da página de Release, o arquivo tem a versão no nome (ex.: `cerne_v0.4.0_darwin_arm64`) — renomeie para `cerne` (ou `cerne.exe`).

## 2. Instalar

### macOS

No terminal, na pasta onde está o arquivo:

```bash
xattr -c cerne          # remove a quarentena do download (senão o macOS bloqueia)
chmod +x cerne
sudo mkdir -p /usr/local/bin
sudo mv cerne /usr/local/bin/
cerne --version
```

Se aparecer *"cerne não pode ser aberto porque a Apple não pode verificá-lo"*, faltou o `xattr -c`: rode `sudo xattr -c /usr/local/bin/cerne`.

### Linux

```bash
chmod +x cerne
sudo mv cerne /usr/local/bin/
cerne --version
```

### Windows (PowerShell)

Na pasta onde está o `cerne.exe`:

```powershell
Unblock-File .\cerne.exe
$dir = "$env:LOCALAPPDATA\Programs\cerne"
New-Item -ItemType Directory -Force $dir | Out-Null
Move-Item .\cerne.exe $dir -Force
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$dir", "User")
```

Feche e abra o terminal, depois `cerne --version`. Se o Windows mostrar *"O Windows protegeu o computador"*, clique em **Mais informações → Executar assim mesmo**.

### Atualizar

Mesmo processo: substitua o executável antigo pelo novo.

## 3. O que você precisa ter

- **git** — recomendado. Sem ele o cerne funciona, mas bem mais devagar.
- **Um repositório git vazio para ser o *backend*** — é onde a documentação mora (JSON + markdown + histórico de eventos). Crie um repo privado vazio no GitHub/GitLab. Autenticação:
  - **SSH** (`git@github.com:voce/meu-projeto-docs.git`): usa suas chaves SSH (`~/.ssh/id_ed25519`, `id_rsa` ou `id_ecdsa`). Confirme que `ssh -T git@github.com` funciona. Chave com senha: carregue antes no agente com `ssh-add`.
  - **HTTPS** (`https://…`): exporte um token em `CERNE_GIT_TOKEN` (e o usuário em `CERNE_GIT_USER`, se o host exigir).
  - **Só testando?** Uma pasta local serve: `git init --bare ~/meu-projeto-docs.git`.
- **Claude Code** (opcional, recomendado) — o cerne foi feito para trabalhar junto com um agente de IA. O `init` já instala as skills e hooks que ensinam o agente a usar o cerne.

## 4. Começar

Dentro do repositório do seu projeto:

```bash
cerne init --backend git@github.com:voce/meu-projeto-docs.git --name meu-projeto
echo ".docs/" >> .gitignore
git add .cernerc docs.lock .gitattributes .gitignore
git commit -m "chore: adota cerne"
```

O dia a dia:

```bash
cerne feature new "Login com Google" --context "Usuários pediram login social."
cerne pull          # materializa o estado atual em .docs/ (é o que o agente lê)
# edite .docs/current/features/F-0001-.../spec.md
cerne push          # envia suas edições de volta ao backend
cerne status        # features por estado (candidata → aprovada → em desenvolvimento → implementada)
cerne ui            # painel web em http://localhost:7717
```

Os avanços de estado (`cerne feature approve`, `start`, `complete`) têm portões: cada um exige o documento da etapa preenchido. Com o Claude Code, peça ao agente — a skill `cerne-authoring` instalada pelo `init` guia o fluxo inteiro. Todos os comandos: `cerne --help`.
