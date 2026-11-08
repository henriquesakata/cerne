import { useRef, useState } from 'react'
import {
  fetchHarness,
  type HarnessItem,
  type HarnessProviderCoverage,
} from './api'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'
import { useT } from './i18n'

// Ordem fixa das classes na tela — espelha o payload do servidor (F-0251).
const CLASS_ORDER = ['cerne', 'hooks', 'skills', 'plugins', 'mcp', 'config', 'context', 'providers']

// Estados que contam como problema para o filtro "só problemas".
const PROBLEM_STATES = new Set(['divergent', 'missing', 'broken'])

// Fornecedores conhecidos, na ordem do payload (F-0259).
const PROVIDERS = ['claude', 'cursor', 'codex', 'gemini']

export default function Harness() {
  const load = useLiveLoad(fetchHarness)
  const inv = load.data
  const [onlyProblems, setOnlyProblems] = useState(false)
  const [onlyOutside, setOnlyOutside] = useState(false)
  const [provider, setProvider] = useState<string | null>(null)
  const { t } = useT()

  if (!inv) return <LoadPending load={load} message={t.harness.loadError} />

  const keep = (it: HarnessItem) =>
    (!onlyProblems || PROBLEM_STATES.has(it.state)) &&
    (!onlyOutside || it.outsideRepo) &&
    (!provider || (it.consumers ?? []).includes(provider))

  const classes = [...inv.classes].sort(
    (a, b) => CLASS_ORDER.indexOf(a.name) - CLASS_ORDER.indexOf(b.name),
  )
  const badLayers = inv.layers.filter((l) => !l.readable && l.error !== 'not present')

  return (
    <main className="page">
      <LoadNotice load={load} />
      <h1>{t.harness.title}</h1>
      {/* Critério 11: "divergente" só faz sentido ancorado na versão que serve. */}
      <p className="muted">{t.harness.versionNote(inv.version)}</p>
      <p className="muted">{t.harness.intro}</p>
      <div className="harness-filters">
        <label>
          <input
            type="checkbox"
            checked={onlyProblems}
            onChange={(e) => setOnlyProblems(e.target.checked)}
          />{' '}
          {t.harness.onlyProblems}
        </label>
        <label>
          <input
            type="checkbox"
            checked={onlyOutside}
            onChange={(e) => setOnlyOutside(e.target.checked)}
          />{' '}
          {t.harness.onlyOutside}
        </label>
        <span className="harness-provider-chips" role="group" aria-label={t.harness.providerFilter}>
          {PROVIDERS.map((p) => (
            <button
              key={p}
              type="button"
              className={`harness-provider-chip${provider === p ? ' active' : ''}`}
              aria-pressed={provider === p}
              onClick={() => setProvider(provider === p ? null : p)}
            >
              {p}
            </button>
          ))}
        </span>
      </div>
      {(inv.coverage ?? []).length > 0 && (
        <section className="entity-group harness-coverage">
          <h2>{t.harness.coverageTitle}</h2>
          <div className="harness-coverage-grid">
            {(inv.coverage ?? []).map((c) => (
              <CoverageCard key={c.provider} cov={c} />
            ))}
          </div>
        </section>
      )}
      {classes.map((c) => {
        const all = c.items ?? []
        if (all.length === 0) return null
        const items = all.filter(keep)
        return (
          <section key={c.name} className="entity-group">
            <h2>
              {t.harness.classLabels[c.name] ?? c.name}{' '}
              <span className="column-count">{items.length}</span>
            </h2>
            {items.length === 0 ? (
              <p className="muted">{t.harness.noneMatch}</p>
            ) : (
              <ul className="harness-list">
                {items.map((it) => (
                  <HarnessRow key={`${c.name}-${it.name}-${it.path}`} item={it} />
                ))}
              </ul>
            )}
          </section>
        )
      })}
      {badLayers.length > 0 && (
        <section className="entity-group">
          <h2>{t.harness.layersTitle}</h2>
          <ul className="harness-list">
            {badLayers.map((l) => (
              <li key={l.path} className="harness-row">
                <span className="card-id">{l.path}</span>
                <span className="lint-detail">{l.error}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </main>
  )
}

// CoverageCard: a fatia de um fornecedor no inventário — total consumível,
// badge de não configurado e as lacunas (degraded/missing) expansíveis.
function CoverageCard({ cov }: { cov: HarnessProviderCoverage }) {
  const { t } = useT()
  const gaps = cov.gaps ?? []
  return (
    <div className="harness-coverage-card">
      <div className="harness-coverage-head">
        <span className="entity-title">{cov.provider}</span>
        {!cov.detected && <span className="tag harness-undetected">{t.harness.notDetected}</span>}
      </div>
      <div className="harness-coverage-total">{t.harness.artifactsCount(cov.total)}</div>
      {gaps.length > 0 && (
        <details>
          <summary>{t.harness.gapsCount(gaps.length)}</summary>
          <ul className="harness-gaps">
            {gaps.map((g) => (
              <li key={`${g.class}-${g.name}-${g.kind}`}>
                <span className={`badge harness-state harness-state-${g.kind}`}>
                  {t.harness.gapKindLabels[g.kind] ?? g.kind}
                </span>{' '}
                <span>{g.name}</span>
                {g.detail && <span className="lint-detail"> — {g.detail}</span>}
                {g.fix && <code className="harness-gap-fix">{g.fix}</code>}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

function HarnessRow({ item }: { item: HarnessItem }) {
  const { t } = useT()
  return (
    <li className="harness-row">
      <div className="harness-row-main">
        <span className={`badge harness-state harness-state-${item.state}`}>
          {t.harness.stateLabels[item.state] ?? item.state}
        </span>
        <span className="entity-title">{item.name}</span>
        <span className="tag">{t.harness.originLabels[item.origin] ?? item.origin}</span>
        {item.outsideRepo && <span className="tag harness-outside">{t.harness.outsideRepo}</span>}
      </div>
      <div className="harness-row-detail">
        <span className="card-id">{item.path}</span>
        {item.detail && <span className="lint-detail">{item.detail}</span>}
      </div>
      {item.fix && <FixBlock command={item.fix} />}
    </li>
  )
}

// FixBlock: o comando de correção em bloco copiável — mesma afordância do
// bloco cercado da F-0247, sempre visível. Sem clipboard (ou com a escrita
// recusada) a cópia diz que falhou e seleciona o texto, para o Cmd/Ctrl+C
// (F-0315, crítica C14); `copyFailed` nomeia o que selecionar. `label` dá a
// cada botão o nome do seu comando quando há vários na tela (F-0334).
export function FixBlock({ command, copyFailed, label }: { command: string; copyFailed?: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'copied' | 'failed'>('idle')
  const pre = useRef<HTMLPreElement>(null)
  const { t } = useT()
  const fail = () => {
    setState('failed')
    if (pre.current) window.getSelection()?.selectAllChildren(pre.current)
  }
  const copy = () => {
    if (!navigator.clipboard) return fail()
    navigator.clipboard.writeText(command).then(() => {
      setState('copied')
      setTimeout(() => setState('idle'), 2000)
    }, fail)
  }
  return (
    <div className="code-block harness-fix">
      <pre ref={pre}>{command}</pre>
      <button type="button" className="code-copy" aria-label={label ?? t.common.copyBlock} onClick={copy}>
        {state === 'copied' ? t.common.copied : t.common.copy}
      </button>
      {state === 'failed' && (
        <p className="form-error code-copy-failed" role="status">
          {copyFailed ?? t.common.copyFailed}
        </p>
      )}
    </div>
  )
}
