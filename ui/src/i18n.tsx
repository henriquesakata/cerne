import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import type {
  FeatureState,
  GraphEdgeType,
  GraphKind,
  LLMMode,
  TransitionAction,
} from './api'

// Hand-rolled i18n (F-0125): the pt-BR dictionary is the source of the
// Messages type, so a key missing from `en` is a compile error. Lifecycle
// state DATA stays English ('candidate', …); only labels translate here.

const ptBR = {
  nav: {
    kanban: 'Kanban',
    manage: 'Gestão',
    decisions: 'Decisões',
    references: 'Referências',
    runbooks: 'Runbooks',
    notes: 'Notas',
    wiki: 'Wiki',
    graph: 'Grafo',
    ask: 'Perguntar',
    search: 'Busca',
    lint: 'Lint',
    history: 'Histórico',
    activity: 'Atividade',
    stats: 'Economia',
    project: 'projeto',
    themeLight: 'Tema claro',
    themeDark: 'Tema escuro',
    language: 'Idioma',
    versionTitle: 'Versão do cerne',
  },
  activity: {
    title: 'Atividade',
    subtitle:
      'O que os agentes estão fazendo agora — registrado via CLI (cerne work start/done); esta aba é somente leitura.',
    loadError: (e: string) => `Erro ao carregar a atividade: ${e}`,
    activeGroup: 'Em andamento',
    finishedGroup: 'Encerradas recentes',
    emptyActive: 'Nenhuma atividade em andamento.',
    stateLabels: {
      active: 'Ativa',
      done: 'Concluída',
      abandoned: 'Abandonada',
    },
  },
  stats: {
    title: 'Economia de contexto',
    subtitle:
      'O que o cerne poupa o agente de ler: o acervo completo (todas as entidades, todos os estados) vs a projeção corrente em .docs/.',
    loadError: (e: string) => `Erro ao carregar as estatísticas: ${e}`,
    savedHeadline: 'do acervo fica fora do contexto do agente',
    corpus: 'Acervo (todos os estados)',
    current: 'Projeção corrente (.docs/)',
    saved: 'Economizado',
    docs: (n: number) => `${n} doc(s)`,
    kindHeader: 'Tipo',
    countHeader: 'Docs',
    bytesHeader: 'Bytes',
    tokensHeader: '~Tokens',
    estimateNote: 'Tokens estimados como bytes/4.',
    empty: 'Backend vazio — nada a medir ainda.',
    kindLabels: {
      feature: 'Features',
      decision: 'Decisões',
      reference: 'Referências',
      runbook: 'Runbooks',
      note: 'Notas',
    },
  },
  state: {
    candidate: 'Candidata',
    approved: 'Aprovada',
    in_development: 'Em desenvolvimento',
    // Jargão kanban mantido em pt (decisão da F-0131): implemented = Ready
    // (aguarda aceite humano), done = Done (validada).
    implemented: 'Ready',
    done: 'Done',
    rejected: 'Rejeitada',
    superseded: 'Substituída',
  } satisfies Record<FeatureState, string>,
  common: {
    loading: 'Carregando…',
    save: 'Salvar',
    cancel: 'Cancelar',
    close: 'Fechar',
    clear: 'Limpar',
    facets: 'Facets',
    components: 'Componentes',
    reasonLabel: 'Motivo:',
    sourcesLabel: 'Fontes:',
    external: 'externa',
    noDoc: 'Sem documento de conteúdo.',
    ofTotal: (total: number) => ` de ${total}`,
  },
  kanban: {
    loadError: (e: string) => `Erro ao carregar o estado: ${e}`,
    counts: (total: number, open: number, closed: number) =>
      `${total} feature(s) · ${open} aberta(s) · ${closed} encerrada(s)`,
    inFilter: (n: number) => ` · ${n} no filtro`,
    groupByEpic: 'Agrupar por épico',
    showClosed: 'Mostrar encerradas',
    columnLabels: {
      in_development: 'Em desenvolvimento',
      approved: 'Aprovadas',
      candidate: 'Candidatas',
      implemented: 'Ready',
      done: 'Done',
      superseded: 'Substituídas',
      rejected: 'Rejeitadas',
    } satisfies Record<FeatureState, string>,
    noEpic: 'Sem épico',
    epicBadge: 'épico',
    childInDev: 'filho em dev',
    childInDevTitle:
      'um filho já está em desenvolvimento; planeje e inicie o épico',
  },
  feature: {
    loadError: (e: string) => `Erro ao carregar a feature: ${e}`,
    backToKanban: '← Kanban',
    editTitle: 'Editar título',
    tabs: { spec: 'Spec', plan: 'Plano', result: 'Resultado' },
    epicLabel: 'Épico:',
    supersededBy: (id: string) => `Substituída por ${id}`,
    convertingToExcalidraw: 'Convertendo para Excalidraw…',
    convertError: (e: string) =>
      `Não foi possível converter o diagrama para Excalidraw: ${e}`,
    edit: 'Editar',
    updatedAgo: (when: string) => `Atualizado ${when}`,
    emptyDoc: 'Vazio — clique em Editar para escrever.',
    diagrams: 'Diagramas',
    generateNeedsMode:
      'A geração automática precisa do modo de IA “assinatura” ou “chave própria”. Troque o modo em Gestão.',
    generating: 'Gerando…',
    generateWithAI: 'Gerar com IA',
    generateDisabledTitle:
      'Disponível nos modos de IA “assinatura” ou “chave própria” — troque o modo em Gestão.',
    diagramNamePlaceholder: 'nome-do-diagrama',
    create: 'Criar',
    newDiagram: 'Novo diagrama',
    actionLabels: {
      approve: 'Aprovar',
      start: 'Iniciar',
      complete: 'Concluir',
      accept: 'Aceitar',
      reject: 'Rejeitar',
      supersede: 'Substituir',
    } satisfies Record<TransitionAction, string>,
    actions: 'Ações',
    closed: 'Encerrada.',
    supersededByField: 'Substituída por (ID)',
    reasonRequired: 'Motivo (obrigatório)',
    confirmAction: (label: string) => `Confirmar ${label.toLowerCase()}`,
    epic: 'Épico',
    childrenProgress: (done: number, total: number) =>
      `${done}/${total} filho(s) implementado(s)`,
    noChildren: 'Sem filhos.',
    noEpicOption: '— sem épico —',
    changeEpic: 'Trocar épico',
    setEpic: 'Definir épico',
  },
  manage: {
    loadError: (e: string) => `Erro ao carregar features: ${e}`,
    title: 'Gestão de features',
    newFeature: '+ Nova feature',
    all: 'Todas',
    close: 'Encerrar…',
    emptyFilter: 'Nenhuma feature neste filtro.',
    quickActionLabels: {
      approve: 'Aprovar',
      start: 'Iniciar',
      complete: 'Concluir',
      accept: 'Aceitar',
    },
  },
  newFeature: {
    backToManage: '← Gestão',
    title: 'Nova feature',
    fieldTitle: 'Título',
    titlePlaceholder: 'título da feature',
    module: 'Módulo',
    required: ' (obrigatório)',
    optional: ' (opcional)',
    epicOptional: 'Épico (opcional)',
    createCandidate: 'Criar (candidata)',
    hint: 'A feature nasce candidata com um stub de spec. Escreva a spec na página dela e aprove quando estiver pronta.',
  },
  decisions: {
    loadError: (e: string) => `Erro ao carregar decisões: ${e}`,
    loadOneError: (e: string) => `Erro ao carregar a decisão: ${e}`,
    title: 'Decisões',
    counts: (n: number) => `${n} decisão(ões)`,
    none: 'Nenhuma decisão registrada.',
    noneMatch: 'Nenhuma decisão corresponde ao filtro.',
    back: '← Decisões',
    groupLabels: { active: 'Ativas', superseded: 'Substituídas' },
    stateLabels: { active: 'Ativa', superseded: 'Substituída' },
    supersededBy: (id: string) => `Substituída por ${id}`,
  },
  references: {
    loadError: (e: string) => `Erro ao carregar referências: ${e}`,
    loadOneError: (e: string) => `Erro ao carregar a referência: ${e}`,
    title: 'Referências',
    counts: (n: number) => `${n} referência(s)`,
    none: 'Nenhuma referência registrada.',
    noneMatch: 'Nenhuma referência corresponde ao filtro.',
    back: '← Referências',
    groupLabels: { current: 'Vigentes', obsolete: 'Obsoletas' },
    stateLabels: { current: 'Vigente', obsolete: 'Obsoleta' },
    newToggle: '+ Nova referência externa',
    formTitle: 'Nova referência externa',
    phTitle: 'Título do artigo/doc',
    phUrl: 'URL (https://…)',
    phContent: 'Conteúdo / resumo (vai pro índice, o ask passa a citar)',
    phInforms: 'Informou (opcional): F-0042, D-0007',
    phFacets: 'Facets (opcional): infra, code',
    phComponents: 'Componentes (opcional): gitea, kpack',
    formError: (e: string) => `Erro: ${e}`,
    registering: 'Registrando…',
    register: 'Registrar',
    sourceLabel: 'Fonte:',
    informsLabel: 'Informou:',
  },
  runbooks: {
    loadError: (e: string) => `Erro ao carregar runbooks: ${e}`,
    loadOneError: (e: string) => `Erro ao carregar o runbook: ${e}`,
    title: 'Runbooks',
    counts: (n: number) => `${n} runbook(s)`,
    none: 'Nenhum runbook registrado.',
    noneMatch: 'Nenhum runbook corresponde ao filtro.',
    back: '← Runbooks',
    groupLabels: { active: 'Ativos', superseded: 'Substituídos' },
    stateLabels: { active: 'Ativo', superseded: 'Substituído' },
    supersededBy: (id: string) => `Substituído por ${id}`,
  },
  notes: {
    loadError: (e: string) => `Erro ao carregar notas: ${e}`,
    loadOneError: (e: string) => `Erro ao carregar a nota: ${e}`,
    title: 'Notas',
    counts: (n: number) => `${n} nota(s)`,
    none: 'Nenhuma nota registrada.',
    noneMatch: 'Nenhuma nota corresponde ao filtro.',
    back: '← Notas',
    groupLabels: { active: 'Ativas', superseded: 'Substituídas' },
    stateLabels: { active: 'Ativa', superseded: 'Substituída' },
    supersededBy: (id: string) => `Substituída por ${id}`,
  },
  wiki: {
    loadError: (e: string) => `Erro ao carregar a wiki: ${e}`,
    loadPageError: (e: string) => `Erro ao carregar a página: ${e}`,
    title: 'Wiki',
    intro1: 'Síntese compilada por ',
    intro2: ', vigente-primeiro. É leitura; a autoria é via CLI/agente.',
    empty1: 'Nenhuma página compilada ainda. Rode ',
    empty2: ' para gerar a síntese.',
    overview: 'Visão geral',
    modules: 'Módulos',
    back: '← Wiki',
    topLabels: {
      project: 'Projeto',
      decisions: 'Decisões',
      references: 'Referências',
    },
  },
  graph: {
    loadingChunk: 'Carregando grafo…',
    loadError: (e: string) => `Erro ao montar o grafo: ${e}`,
    title: 'Grafo',
    stats: (nodes: number, shown: number, total: number) =>
      `${nodes} nós · ${shown}/${total} arestas`,
    layouting: ' · calculando layout…',
    edges: 'Arestas',
    kindLabels: {
      feature: 'feature',
      decision: 'decisão',
      reference: 'referência',
      historical: 'histórico',
    } satisfies Record<GraphKind, string>,
    edgeLabels: {
      superseded_by: 'substituída por',
      part_of: 'parte de',
      informs: 'informa',
      links: 'cita',
    } satisfies Record<GraphEdgeType, string>,
    conn: (type: GraphEdgeType, dir: 'out' | 'in', other: string): string => {
      switch (type) {
        case 'superseded_by':
          return dir === 'out' ? `substituída por ${other}` : `substitui ${other}`
        case 'informs':
          return dir === 'out' ? `informa ${other}` : `fonte ${other}`
        case 'links':
          return dir === 'out' ? `cita ${other}` : `citada por ${other}`
        case 'part_of':
          return dir === 'out' ? `parte de ${other}` : `contém ${other}`
      }
    },
    openPage: 'abrir página →',
    connections: 'Conexões',
    noConnections: 'Sem conexões.',
    goTo: (id: string) => `ir para ${id}`,
    reasonPrefix: 'motivo:',
    clickHint:
      'Clique num nó para desenhar e listar suas conexões — ou ligue um tipo de aresta acima para ver toda a malha.',
    nodeTypes: 'Tipos de nó',
  },
  ask: {
    modeLabels: {
      delegation: 'Delegação (o agente responde)',
      'claude-cli': 'Assinatura (claude-cli)',
      'byo-key': 'Chave de API (byo-key)',
    } satisfies Record<LLMMode, string>,
    aiMode: 'Modo de IA:',
    errPrefix: (e: string) => `erro: ${e}`,
    loadingLower: 'carregando…',
    byoKeyHint: ' requer a chave no env configurado no .cernerc',
    kindLabels: {
      feature: 'feature',
      decision: 'decisão',
      reference: 'referência',
      wiki: 'wiki',
    },
    noSources: 'Nenhuma fonte recuperada.',
    notice1: 'Modo ',
    noticeMode: 'delegação',
    notice2:
      ': nesse modo quem responde é o agente, não o servidor — então aqui só mostramos as fontes recuperadas (abaixo). Para respostas em linguagem natural, troque o ',
    noticeAiMode: 'Modo de IA',
    notice3: ' acima para ',
    noticeSub: 'Assinatura',
    notice4: ' ou ',
    noticeKey: 'Chave de API',
    notice5: ' e pergunte de novo.',
    sourcesTitle: 'Fontes',
    title: 'Perguntar',
    intro1:
      'Pergunte em linguagem natural sobre o estado vigente do projeto — a resposta vem do provider configurado (assinatura/byo-key). Para busca por palavra-chave, use ',
    introLink: 'Busca',
    intro2: '.',
    placeholder: 'Como funciona o SSO entre os apps?',
    button: 'Perguntar',
    includeHistory: 'Incluir histórico (substituídas, rejeitadas, obsoletas)',
    querying: 'Consultando…',
    askError: (e: string) => `Erro ao perguntar: ${e}`,
  },
  search: {
    kindLabels: {
      feature: 'feature',
      decision: 'decisão',
      reference: 'referência',
    },
    title: 'Busca',
    intro:
      'Busca full-text sobre todo o histórico (inclui substituídas e rejeitadas).',
    placeholder: 'termo…',
    button: 'Buscar',
    error: (e: string) => `Erro na busca: ${e}`,
    nothing: 'Nada encontrado.',
  },
  lint: {
    rules: {
      'ref-to-superseded': 'Menção a entidade substituída',
      'broken-link': 'Link quebrado',
      'orphan-content': 'Conteúdo órfão',
      'missing-gate-doc': 'Documento de gate ausente',
      'wiki-source-unknown': 'Fonte de wiki desconhecida',
      'parent-not-found': 'Épico inexistente',
      'parent-terminal': 'Feature sob épico terminal',
      'epic-open-children': 'Épico concluído com filhos abertos',
      'stale-candidate': 'Candidata parada (sem spec)',
      'stale-note': 'Nota obsoleta (trabalho informado encerrado)',
    },
    error: (e: string) => `Erro ao rodar o lint: ${e}`,
    title: 'Lint',
    noProblems: 'Nenhum problema encontrado.',
    counts: (n: number) => `${n} problema(s)`,
  },
  history: {
    typeLabels: {
      'feature.created': 'criada',
      'feature.approved': 'aprovada',
      'feature.started': 'iniciada',
      'feature.completed': 'concluída',
      'feature.rejected': 'rejeitada',
      'feature.superseded': 'substituída',
      'feature.classified': 'classificada',
    },
    error: (e: string) => `Erro ao carregar o histórico: ${e}`,
    title: 'Histórico',
    intro: 'Log de eventos do ciclo de vida, mais recente primeiro.',
    none: 'Nenhum evento registrado.',
  },
  diagram: {
    loadError: (e: string) => `Erro ao carregar o diagrama: ${e}`,
    saved: 'Diagrama salvo.',
    save: 'Salvar diagrama',
    loading: 'Carregando diagrama…',
    rendering: 'Renderizando diagrama…',
    editAsExcalidraw: 'Editar como Excalidraw',
    mermaidError: (e: string) => `Não foi possível renderizar o Mermaid: ${e}`,
    close: 'Fechar',
  },
  live: {
    loading: 'Carregando…',
    useProjectsError: 'useProjects deve ser usado dentro de LiveProvider',
  },
}

export type Messages = typeof ptBR

const en: Messages = {
  nav: {
    kanban: 'Kanban',
    manage: 'Manage',
    decisions: 'Decisions',
    references: 'References',
    runbooks: 'Runbooks',
    notes: 'Notes',
    wiki: 'Wiki',
    graph: 'Graph',
    ask: 'Ask',
    search: 'Search',
    lint: 'Lint',
    history: 'History',
    activity: 'Activity',
    stats: 'Savings',
    project: 'project',
    themeLight: 'Light theme',
    themeDark: 'Dark theme',
    language: 'Language',
    versionTitle: 'cerne version',
  },
  activity: {
    title: 'Activity',
    subtitle:
      'What agents are working on right now — registered via CLI (cerne work start/done); this tab is read-only.',
    loadError: (e: string) => `Failed to load activity: ${e}`,
    activeGroup: 'In progress',
    finishedGroup: 'Recently finished',
    emptyActive: 'No activity in progress.',
    stateLabels: {
      active: 'Active',
      done: 'Done',
      abandoned: 'Abandoned',
    },
  },
  stats: {
    title: 'Context savings',
    subtitle:
      'What cerne keeps out of the agent’s reading: the full corpus (every entity, every state) vs the current .docs/ projection.',
    loadError: (e: string) => `Failed to load stats: ${e}`,
    savedHeadline: 'of the corpus stays out of the agent’s context',
    corpus: 'Corpus (all states)',
    current: 'Current projection (.docs/)',
    saved: 'Saved',
    docs: (n: number) => `${n} doc(s)`,
    kindHeader: 'Kind',
    countHeader: 'Docs',
    bytesHeader: 'Bytes',
    tokensHeader: '~Tokens',
    estimateNote: 'Tokens estimated as bytes/4.',
    empty: 'Empty backend — nothing to measure yet.',
    kindLabels: {
      feature: 'Features',
      decision: 'Decisions',
      reference: 'References',
      runbook: 'Runbooks',
      note: 'Notes',
    },
  },
  state: {
    candidate: 'Candidate',
    approved: 'Approved',
    in_development: 'In development',
    implemented: 'Ready',
    done: 'Done',
    rejected: 'Rejected',
    superseded: 'Superseded',
  },
  common: {
    loading: 'Loading…',
    save: 'Save',
    cancel: 'Cancel',
    close: 'Close',
    clear: 'Clear',
    facets: 'Facets',
    components: 'Components',
    reasonLabel: 'Reason:',
    sourcesLabel: 'Sources:',
    external: 'external',
    noDoc: 'No content document.',
    ofTotal: (total: number) => ` of ${total}`,
  },
  kanban: {
    loadError: (e: string) => `Failed to load state: ${e}`,
    counts: (total: number, open: number, closed: number) =>
      `${total} feature(s) · ${open} open · ${closed} closed`,
    inFilter: (n: number) => ` · ${n} in filter`,
    groupByEpic: 'Group by epic',
    showClosed: 'Show closed',
    columnLabels: {
      in_development: 'In development',
      approved: 'Approved',
      candidate: 'Candidates',
      implemented: 'Ready',
      done: 'Done',
      superseded: 'Superseded',
      rejected: 'Rejected',
    },
    noEpic: 'No epic',
    epicBadge: 'epic',
    childInDev: 'child in dev',
    childInDevTitle:
      'a child is already in development; plan and start the epic',
  },
  feature: {
    loadError: (e: string) => `Failed to load the feature: ${e}`,
    backToKanban: '← Kanban',
    editTitle: 'Edit title',
    tabs: { spec: 'Spec', plan: 'Plan', result: 'Result' },
    epicLabel: 'Epic:',
    supersededBy: (id: string) => `Superseded by ${id}`,
    convertingToExcalidraw: 'Converting to Excalidraw…',
    convertError: (e: string) =>
      `Could not convert the diagram to Excalidraw: ${e}`,
    edit: 'Edit',
    updatedAgo: (when: string) => `Updated ${when}`,
    emptyDoc: 'Empty — click Edit to write.',
    diagrams: 'Diagrams',
    generateNeedsMode:
      'Automatic generation needs the “subscription” or “own key” AI mode. Switch the mode in Manage.',
    generating: 'Generating…',
    generateWithAI: 'Generate with AI',
    generateDisabledTitle:
      'Available in the “subscription” or “own key” AI modes — switch the mode in Manage.',
    diagramNamePlaceholder: 'diagram-name',
    create: 'Create',
    newDiagram: 'New diagram',
    actionLabels: {
      approve: 'Approve',
      start: 'Start',
      complete: 'Complete',
      accept: 'Accept',
      reject: 'Reject',
      supersede: 'Supersede',
    },
    actions: 'Actions',
    closed: 'Closed.',
    supersededByField: 'Superseded by (ID)',
    reasonRequired: 'Reason (required)',
    confirmAction: (label: string) => `Confirm ${label.toLowerCase()}`,
    epic: 'Epic',
    childrenProgress: (done: number, total: number) =>
      `${done}/${total} child(ren) implemented`,
    noChildren: 'No children.',
    noEpicOption: '— no epic —',
    changeEpic: 'Change epic',
    setEpic: 'Set epic',
  },
  manage: {
    loadError: (e: string) => `Failed to load features: ${e}`,
    title: 'Feature management',
    newFeature: '+ New feature',
    all: 'All',
    close: 'Close…',
    emptyFilter: 'No feature in this filter.',
    quickActionLabels: {
      approve: 'Approve',
      start: 'Start',
      complete: 'Complete',
      accept: 'Accept',
    },
  },
  newFeature: {
    backToManage: '← Manage',
    title: 'New feature',
    fieldTitle: 'Title',
    titlePlaceholder: 'feature title',
    module: 'Module',
    required: ' (required)',
    optional: ' (optional)',
    epicOptional: 'Epic (optional)',
    createCandidate: 'Create (candidate)',
    hint: 'The feature is born a candidate with a spec stub. Write the spec on its page and approve it when ready.',
  },
  decisions: {
    loadError: (e: string) => `Failed to load decisions: ${e}`,
    loadOneError: (e: string) => `Failed to load the decision: ${e}`,
    title: 'Decisions',
    counts: (n: number) => `${n} decision(s)`,
    none: 'No decision recorded.',
    noneMatch: 'No decision matches the filter.',
    back: '← Decisions',
    groupLabels: { active: 'Active', superseded: 'Superseded' },
    stateLabels: { active: 'Active', superseded: 'Superseded' },
    supersededBy: (id: string) => `Superseded by ${id}`,
  },
  references: {
    loadError: (e: string) => `Failed to load references: ${e}`,
    loadOneError: (e: string) => `Failed to load the reference: ${e}`,
    title: 'References',
    counts: (n: number) => `${n} reference(s)`,
    none: 'No reference registered.',
    noneMatch: 'No reference matches the filter.',
    back: '← References',
    groupLabels: { current: 'Current', obsolete: 'Obsolete' },
    stateLabels: { current: 'Current', obsolete: 'Obsolete' },
    newToggle: '+ New external reference',
    formTitle: 'New external reference',
    phTitle: 'Article/doc title',
    phUrl: 'URL (https://…)',
    phContent: 'Content / summary (goes into the index; ask starts citing it)',
    phInforms: 'Informed (optional): F-0042, D-0007',
    phFacets: 'Facets (optional): infra, code',
    phComponents: 'Components (optional): gitea, kpack',
    formError: (e: string) => `Error: ${e}`,
    registering: 'Registering…',
    register: 'Register',
    sourceLabel: 'Source:',
    informsLabel: 'Informed:',
  },
  runbooks: {
    loadError: (e: string) => `Failed to load runbooks: ${e}`,
    loadOneError: (e: string) => `Failed to load the runbook: ${e}`,
    title: 'Runbooks',
    counts: (n: number) => `${n} runbook(s)`,
    none: 'No runbook registered.',
    noneMatch: 'No runbook matches the filter.',
    back: '← Runbooks',
    groupLabels: { active: 'Active', superseded: 'Superseded' },
    stateLabels: { active: 'Active', superseded: 'Superseded' },
    supersededBy: (id: string) => `Superseded by ${id}`,
  },
  notes: {
    loadError: (e: string) => `Failed to load notes: ${e}`,
    loadOneError: (e: string) => `Failed to load the note: ${e}`,
    title: 'Notes',
    counts: (n: number) => `${n} note(s)`,
    none: 'No note recorded.',
    noneMatch: 'No note matches the filter.',
    back: '← Notes',
    groupLabels: { active: 'Active', superseded: 'Superseded' },
    stateLabels: { active: 'Active', superseded: 'Superseded' },
    supersededBy: (id: string) => `Superseded by ${id}`,
  },
  wiki: {
    loadError: (e: string) => `Failed to load the wiki: ${e}`,
    loadPageError: (e: string) => `Failed to load the page: ${e}`,
    title: 'Wiki',
    intro1: 'Synthesis compiled by ',
    intro2: ', current-first. Read-only; authoring goes through the CLI/agent.',
    empty1: 'No page compiled yet. Run ',
    empty2: ' to generate the synthesis.',
    overview: 'Overview',
    modules: 'Modules',
    back: '← Wiki',
    topLabels: {
      project: 'Project',
      decisions: 'Decisions',
      references: 'References',
    },
  },
  graph: {
    loadingChunk: 'Loading graph…',
    loadError: (e: string) => `Failed to build the graph: ${e}`,
    title: 'Graph',
    stats: (nodes: number, shown: number, total: number) =>
      `${nodes} nodes · ${shown}/${total} edges`,
    layouting: ' · computing layout…',
    edges: 'Edges',
    kindLabels: {
      feature: 'feature',
      decision: 'decision',
      reference: 'reference',
      historical: 'historical',
    },
    edgeLabels: {
      superseded_by: 'superseded by',
      part_of: 'part of',
      informs: 'informs',
      links: 'cites',
    },
    conn: (type: GraphEdgeType, dir: 'out' | 'in', other: string): string => {
      switch (type) {
        case 'superseded_by':
          return dir === 'out' ? `superseded by ${other}` : `supersedes ${other}`
        case 'informs':
          return dir === 'out' ? `informs ${other}` : `source ${other}`
        case 'links':
          return dir === 'out' ? `cites ${other}` : `cited by ${other}`
        case 'part_of':
          return dir === 'out' ? `part of ${other}` : `contains ${other}`
      }
    },
    openPage: 'open page →',
    connections: 'Connections',
    noConnections: 'No connections.',
    goTo: (id: string) => `go to ${id}`,
    reasonPrefix: 'reason:',
    clickHint:
      'Click a node to draw and list its connections — or turn on an edge type above to see the whole mesh.',
    nodeTypes: 'Node types',
  },
  ask: {
    modeLabels: {
      delegation: 'Delegation (the agent answers)',
      'claude-cli': 'Subscription (claude-cli)',
      'byo-key': 'API key (byo-key)',
    },
    aiMode: 'AI mode:',
    errPrefix: (e: string) => `error: ${e}`,
    loadingLower: 'loading…',
    byoKeyHint: ' requires the key in the env configured in .cernerc',
    kindLabels: {
      feature: 'feature',
      decision: 'decision',
      reference: 'reference',
      wiki: 'wiki',
    },
    noSources: 'No source retrieved.',
    notice1: 'Mode ',
    noticeMode: 'delegation',
    notice2:
      ': in this mode the agent answers, not the server — so here we only show the retrieved sources (below). For natural-language answers, switch the ',
    noticeAiMode: 'AI mode',
    notice3: ' above to ',
    noticeSub: 'Subscription',
    notice4: ' or ',
    noticeKey: 'API key',
    notice5: ' and ask again.',
    sourcesTitle: 'Sources',
    title: 'Ask',
    intro1:
      'Ask in natural language about the current state of the project — the answer comes from the configured provider (subscription/byo-key). For keyword search, use ',
    introLink: 'Search',
    intro2: '.',
    placeholder: 'How does SSO work across the apps?',
    button: 'Ask',
    includeHistory: 'Include history (superseded, rejected, obsolete)',
    querying: 'Querying…',
    askError: (e: string) => `Failed to ask: ${e}`,
  },
  search: {
    kindLabels: {
      feature: 'feature',
      decision: 'decision',
      reference: 'reference',
    },
    title: 'Search',
    intro: 'Full-text search over the whole history (includes superseded and rejected).',
    placeholder: 'term…',
    button: 'Search',
    error: (e: string) => `Search failed: ${e}`,
    nothing: 'Nothing found.',
  },
  lint: {
    rules: {
      'ref-to-superseded': 'Mention of a superseded entity',
      'broken-link': 'Broken link',
      'orphan-content': 'Orphan content',
      'missing-gate-doc': 'Missing gate document',
      'wiki-source-unknown': 'Unknown wiki source',
      'parent-not-found': 'Missing epic',
      'parent-terminal': 'Feature under a terminal epic',
      'epic-open-children': 'Epic completed with open children',
      'stale-candidate': 'Stalled candidate (no spec)',
      'stale-note': 'Stale note (informed work closed)',
    },
    error: (e: string) => `Failed to run lint: ${e}`,
    title: 'Lint',
    noProblems: 'No problem found.',
    counts: (n: number) => `${n} problem(s)`,
  },
  history: {
    typeLabels: {
      'feature.created': 'created',
      'feature.approved': 'approved',
      'feature.started': 'started',
      'feature.completed': 'completed',
      'feature.rejected': 'rejected',
      'feature.superseded': 'superseded',
      'feature.classified': 'classified',
    },
    error: (e: string) => `Failed to load history: ${e}`,
    title: 'History',
    intro: 'Lifecycle event log, most recent first.',
    none: 'No event recorded.',
  },
  diagram: {
    loadError: (e: string) => `Failed to load the diagram: ${e}`,
    saved: 'Diagram saved.',
    save: 'Save diagram',
    loading: 'Loading diagram…',
    rendering: 'Rendering diagram…',
    editAsExcalidraw: 'Edit as Excalidraw',
    mermaidError: (e: string) => `Could not render the Mermaid diagram: ${e}`,
    close: 'Close',
  },
  live: {
    loading: 'Loading…',
    useProjectsError: 'useProjects must be used inside LiveProvider',
  },
}

export type Lang = 'pt-BR' | 'en'

const STORAGE_KEY = 'cerne-lang'
const DICTS: Record<Lang, Messages> = { 'pt-BR': ptBR, en }

function detectLang(): Lang {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === 'pt-BR' || saved === 'en') return saved
  return navigator.language.toLowerCase().startsWith('pt') ? 'pt-BR' : 'en'
}

type I18n = { t: Messages; lang: Lang; setLang: (l: Lang) => void }

const Ctx = createContext<I18n>({ t: ptBR, lang: 'pt-BR', setLang: () => {} })

export function I18nProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detectLang)

  useEffect(() => {
    document.documentElement.lang = lang
  }, [lang])

  const setLang = (l: Lang) => {
    localStorage.setItem(STORAGE_KEY, l)
    setLangState(l)
  }

  return (
    <Ctx.Provider value={{ t: DICTS[lang], lang, setLang }}>
      {children}
    </Ctx.Provider>
  )
}

export function useT(): I18n {
  return useContext(Ctx)
}
