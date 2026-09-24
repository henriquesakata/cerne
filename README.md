# cerne

> O *cerne* é o lenho duro no centro da árvore — a parte que permanece enquanto a árvore cresce.

**cerne** gerencia a documentação viva de projetos de software desenvolvidos com agentes de IA: separa o que está **vigente** do que é **histórico**, materializa apenas o estado atual no repositório do usuário e mantém o ciclo de vida de features (candidata → aprovada → em desenvolvimento → implementada) como dado estruturado — nunca prosa.

## O problema

Documentação de projeto é append-only: specs, planos, ADRs e resultados se acumulam, e nada distingue mecanicamente o documento que vale hoje daquele que foi rejeitado ou substituído há seis meses. Agentes de IA leem tudo com o mesmo peso — e citam estruturas que não existem mais.

## A tese

A união de três coisas que nenhuma ferramenta existente combina:

1. **Ciclo de vida estruturado** — estado de features e decisões como dado de primeira classe, com transições validadas e log de eventos (Jira tem isso; RAG não).
2. **Materialização do vigente** — uma pasta `.docs/` gitignored no repo do usuário contendo *só* o estado atual + um `docs.lock` commitado amarrando código a docs (docs-as-code tem arquivos; não tem estado).
3. **Camada compilada para agentes** — wiki de síntese + `CONTEXT.md` gerado, no espírito do [LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) de Karpathy — mas com lint de obsolescência **determinístico**, porque o ciclo de vida é dado, não texto.

## Instalar

Binário único para macOS, Linux e Windows — veja [INSTALL.md](INSTALL.md).

## Estado

Em uso diário (este repositório documenta a si mesmo com o cerne). Documento fundador: [docs/process/cases/0001_founding-spec/spec.md](docs/process/cases/0001_founding-spec/spec.md).
