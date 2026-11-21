import {
  createContext,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import {
  fetchProjects,
  fetchUpdate,
  restartPanel,
  setApiProject,
  startUpdate,
  subscribeLive,
  type ModelsChange,
  type PanelIdentity,
  type ProjectInfo,
  type UpdateState,
} from './api'
import { useT } from './i18n'
import { navAllowed, setNavGuard } from './navGuard'

// Multi-projeto + atualização ao vivo (casos 0031/0032). O painel serve N
// projetos; este provider mantém a lista, o projeto ativo, o conjunto de
// projetos com mudanças pendentes (badge) e um contador de versão que as telas
// usam para re-buscar.
//
// O projeto ativo é escopado por ABA (F-0288): sessionStorage guarda a escolha
// desta aba (isolada, sobrevive a reload) e localStorage guarda apenas o
// default de uma aba nova (última escolha vence). Antes a chave vivia só no
// localStorage com um listener de `storage` sincronizando as abas — o que
// tornava impossível acompanhar dois projetos em paralelo.

const STORAGE_KEY = 'cerne.activePid'

// pageReload recarrega a aba; um objeto para os testes trocarem a ação
// (o jsdom não navega).
export const pageReload = { run: () => window.location.reload() }

// Sem o "v" das tags, como o selo de versão da barra mostra.
const shortVersion = (v: string) => v.replace(/^v/, '')

type ProjectsState = {
  projects: ProjectInfo[]
  activePid: string
  setActivePid: (pid: string) => void
  changed: Set<string>
  version: number
  // Último change do SSE com o pid de origem (F-0161): aditivo — permite a
  // camada de notificações observar projetos que não são o ativo.
  lastChange: { pid: string; seq: number }
  // Último refresh do catálogo de modelos anunciado (F-0311): global, sem pid.
  modelsChange: ModelsChange | null
  // F-0480: a release nova e a troca do binário pelo painel (o botão mora no Nav).
  update: UpdateState | null
  // F-0492: o botão da barra só pergunta; a troca começa no "Atualizar agora".
  askUpdate: () => void
  toggleUpdateFix: () => void
}

const Ctx = createContext<ProjectsState | null>(null)

export function LiveProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<ProjectInfo[]>([])
  const [activePid, setActivePidState] = useState('')
  const [changed, setChanged] = useState<Set<string>>(() => new Set())
  const [version, setVersion] = useState(0)
  const [lastChange, setLastChange] = useState({ pid: '', seq: 0 })
  const [modelsChange, setModelsChange] = useState<ModelsChange | null>(null)
  const [ready, setReady] = useState(false)
  const [bootError, setBootError] = useState<string | null>(null)
  // A failed re-read AFTER the boot (F-0315, critic C2): every settings write
  // sends the "projects" event, so a flaky re-read must not swap the whole app
  // for the boot-error page — the current list and screens stay, with a notice.
  const [refreshError, setRefreshError] = useState<string | null>(null)
  const booted = useRef(false)
  const { t } = useT()

  // F-0478: o binário que serve o painel e o que o comando dele resolve hoje
  // no disco. A identidade da subida fica guardada: quando o painel volta de
  // um reinício com outra, o código desta página é de outra versão.
  const [panel, setPanel] = useState<{ running: PanelIdentity; installed?: PanelIdentity } | null>(null)
  const bootIdentity = useRef('')
  const [restart, setRestart] = useState<{ state: 'idle' | 'restarting' | 'failed'; error?: string }>({
    state: 'idle',
  })
  const [updatedTo, setUpdatedTo] = useState('')
  // F-0480: estado da release nova, a última linha da troca em curso, a recusa
  // do POST e o aviso com o comando de quem não troca o binário por dentro.
  const [update, setUpdate] = useState<UpdateState | null>(null)
  const [updateLine, setUpdateLine] = useState('')
  const [updateError, setUpdateError] = useState('')
  const [showUpdateFix, setShowUpdateFix] = useState(false)
  // F-0492: a pergunta antes da troca, e o binário do disco cujo aviso de
  // reinício a pessoa adiou nesta aba ("Mais tarde"; nada é gravado).
  const [confirmUpdate, setConfirmUpdate] = useState(false)
  const [restartLater, setRestartLater] = useState('')
  // Prazo da recarga pendente: o painel volta antes de os processos acoplados
  // registrarem de novo os seus projetos.
  const reloadBy = useRef(0)
  // Um reinício aceito que não traz o painel de volta noutra versão (o exec
  // falhou e o processo seguiu na versão de antes) não pode deixar o aviso
  // em "Reiniciando…" para sempre.
  const restartTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  useEffect(() => () => clearTimeout(restartTimer.current), [])

  // activeRef espelha o pid ativo para uso síncrono fora do ciclo de render
  // (callbacks de SSE e o reload da lista), evitando closures velhas.
  const activeRef = useRef('')

  // O link do `cerne ui` (?project=<pid>, F-0373) é lido uma vez no mount e
  // consumido no primeiro load da lista: sai da URL, para que reload e
  // navegação depois sigam a escolha do usuário.
  const linkedRef = useRef(new URLSearchParams(window.location.search).get('project') ?? '')
  const [linkMissing, setLinkMissing] = useState('')

  // apply troca o projeto ativo de forma síncrona: atualiza o ref, a base da
  // API (antes de qualquer refetch) e o estado; só bump de versão quando muda.
  const apply = (pid: string) => {
    if (!pid || pid === activeRef.current) return
    activeRef.current = pid
    setApiProject(pid)
    setActivePidState(pid)
    setVersion((v) => v + 1)
  }

  const pick = (list: ProjectInfo[], def: string): string => {
    const has = (p: string) => list.some((x) => x.pid === p)
    if (activeRef.current && has(activeRef.current)) return activeRef.current
    // Link do `cerne ui` ao entrar num painel aberto (F-0373): vence a escolha
    // guardada, mas só no boot (acima, activeRef já manda nos reloads).
    const linked = linkedRef.current
    if (linked && has(linked)) return linked
    const mine = sessionStorage.getItem(STORAGE_KEY) ?? '' // escolha desta aba
    if (mine && has(mine)) return mine
    const last = localStorage.getItem(STORAGE_KEY) ?? '' // default de aba nova
    if (last && has(last)) return last
    if (def && has(def)) return def
    return list[0]?.pid ?? ''
  }

  // reload nunca fica true em falha do bootstrap (F-0242 critério 6): pid
  // vazio com ready=true liberava as telas para montar chamadas /projects//,
  // 404 permanente. Sucesso com lista vazia é estado legítimo, não erro —
  // ready=true renderiza a mensagem de zero-projetos em vez dos children.
  const reload = () => {
    void fetchUpdate()
      .then((u) => {
        setUpdate(u)
        // F-0492: uma troca começada por outro caminho (ou a versão nova que
        // sumiu) fecha a pergunta, que não volta sozinha quando ela acaba.
        if (!u?.newer || !u.swappable || u.running || !u.latest) setConfirmUpdate(false)
      })
      .catch(() => {
        setUpdate(null) // sem estado, sem botão: a falha não vira erro de UI
        setConfirmUpdate(false)
      })
    return loadProjects()
  }
  const loadProjects = () =>
    fetchProjects()
      .then(({ projects, default: def, version, ui, installed }) => {
        if (version !== undefined) {
          const id = `${version}|${ui ?? ''}`
          if (!bootIdentity.current) bootIdentity.current = id
          else if (id !== bootIdentity.current) {
            // Espera o projeto desta aba voltar à lista, para a recarga cair
            // nele; um projeto que não volta não segura a recarga mais de 15 s.
            // Até lá a lista parcial não entra, senão a aba trocaria de projeto.
            const back = projects.some((p) => p.pid === activeRef.current)
            if (!back && !reloadBy.current) {
              reloadBy.current = Date.now() + 15_000
              setTimeout(() => void reload(), 15_100)
            }
            if (!back && Date.now() < reloadBy.current) return
            reloadBy.current = 0
            // A recarga passa pela trava do rascunho do cofre (F-0464): recusada,
            // a página fica e o aviso oferece recarregar depois.
            if (navAllowed()) {
              setNavGuard(null)
              // A aba que veio pelo link do `cerne ui` não guardou escolha: sem
              // isto a recarga cairia no projeto padrão do painel.
              if (activeRef.current) sessionStorage.setItem(STORAGE_KEY, activeRef.current)
              pageReload.run()
              return
            }
            setUpdatedTo(version)
          }
          setPanel({ running: { version, ui: ui ?? '' }, installed })
        }
        setProjects(projects)
        apply(pick(projects, def))
        if (linkedRef.current) {
          if (!projects.some((x) => x.pid === linkedRef.current)) setLinkMissing(linkedRef.current)
          linkedRef.current = ''
          const u = new URL(window.location.href)
          u.searchParams.delete('project')
          window.history.replaceState(window.history.state, '', u.pathname + u.search + u.hash)
        }
        // Limpa badges de projetos que saíram do painel.
        setChanged((s) => new Set([...s].filter((p) => projects.some((x) => x.pid === p))))
        setBootError(null)
        setRefreshError(null)
        setReady(true)
        booted.current = true
      })
      .catch((e: unknown) => {
        const message = e instanceof Error ? e.message : String(e)
        if (booted.current) setRefreshError(message)
        else setBootError(message)
      })

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    return subscribeLive({
      onChange: (pid) => {
        setLastChange((c) => ({ pid, seq: c.seq + 1 }))
        if (pid === activeRef.current) setVersion((v) => v + 1)
        else setChanged((s) => new Set(s).add(pid))
        // A backend advance may carry a rename pulled from another clone
        // (F-0331 critic C1): the pull tick only sends "change", so re-read
        // the list too, or the selector and the name field go stale.
        void reload()
      },
      onProjects: () => void reload(),
      onModels: setModelsChange,
      onUpdate: setUpdateLine,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const setActivePid = (pid: string) => {
    // An unsaved vault draft asks first, as any route change does (F-0464,
    // audit A2): refused, nothing switches; confirmed, the draft is
    // discarded, so the router's own check that follows does not ask again.
    if (!navAllowed()) return
    setNavGuard(null)
    sessionStorage.setItem(STORAGE_KEY, pid) // esta aba
    localStorage.setItem(STORAGE_KEY, pid) // default da próxima aba
    setChanged((s) => {
      const n = new Set(s)
      n.delete(pid)
      return n
    })
    apply(pid)
    window.location.hash = ''
  }

  if (bootError) {
    return (
      <p className="status error">
        {t.live.bootError(bootError)}{' '}
        <button type="button" onClick={() => void reload()}>
          {t.live.retry}
        </button>
      </p>
    )
  }
  const doRestart = () => {
    if (restart.state === 'restarting') return
    setRestart({ state: 'restarting' })
    restartTimer.current = setTimeout(() => setRestart({ state: 'failed', error: t.live.restartTimedOut }), 30_000)
    restartPanel().catch((e: unknown) => {
      clearTimeout(restartTimer.current)
      setRestart({ state: 'failed', error: e instanceof Error ? e.message : String(e) })
    })
  }
  const beginUpdate = () => {
    setConfirmUpdate(false)
    if (update?.running) return
    setUpdateError('')
    setUpdateLine('')
    setUpdate((u) => (u ? { ...u, running: true, last: undefined } : u))
    startUpdate().catch((e: unknown) => {
      setUpdateError(e instanceof Error ? e.message : String(e))
      void reload()
    })
  }
  // Uma troca que não instalou nada (falhou, ou o gerenciador não achou versão
  // nova) só vale enquanto ainda há versão nova: resolvida por fora, o aviso sai.
  const updateFailed =
    !update?.running && update?.newer && update.last && update.last.state !== 'updated' ? update.last : null
  const updateNoRestart = !update?.running && update?.last?.state === 'updated' ? update.last.restart_error : ''
  const installed = panel?.installed
  // Pelo arquivo, não pela versão: um build local novo pode ter a mesma versão
  // e a mesma UI (critério 1).
  const installedKey = installed ? `${installed.version}|${installed.ui}` : ''
  const newBinary = !!installed?.changed && !!installed.version && !installed.error && installedKey !== restartLater
  const askingUpdate = confirmUpdate && !!update?.newer && update.swappable && !update.running && !!update.latest
  const postponeRestart = () => {
    clearTimeout(restartTimer.current)
    setRestart({ state: 'idle' })
    setRestartLater(installedKey)
  }

  if (!ready) return <p className="status">{t.live.loading}</p>
  if (projects.length === 0) return <p className="status">{t.live.noProjects}</p>

  return (
    <Ctx.Provider
      value={{
        projects,
        activePid,
        setActivePid,
        changed,
        version,
        lastChange,
        modelsChange,
        update,
        askUpdate: () => setConfirmUpdate(true),
        toggleUpdateFix: () => setShowUpdateFix((v) => !v),
      }}
    >
      {askingUpdate && (
        <div className="load-notice live-refresh-notice advisory" role="status">
          <p>{t.live.updateConfirm(shortVersion(update.current), shortVersion(update.latest ?? ''))}</p>
          <button type="button" onClick={beginUpdate}>
            {t.live.updateNow}
          </button>
          <button type="button" onClick={() => setConfirmUpdate(false)}>
            {t.live.later}
          </button>
        </div>
      )}
      {update?.running && (
        <div className="load-notice live-refresh-notice advisory" role="status">
          <p>
            {t.live.updating}
            {updateLine && <span className="update-line"> {updateLine}</span>}
          </p>
        </div>
      )}
      {(updateFailed || updateError) && (
        <div className="load-notice live-refresh-notice" role="status">
          <p className="form-error">{t.live.updateFailed(updateError || updateFailed?.detail || '')}</p>
          {updateFailed?.fix && (
            <p>
              {t.live.runInTerminal} <code>{updateFailed.fix}</code>
            </p>
          )}
        </div>
      )}
      {updateNoRestart && (
        <div className="load-notice live-refresh-notice" role="status">
          <p className="form-error">{t.live.updateNoRestart(updateNoRestart)}</p>
        </div>
      )}
      {showUpdateFix && update?.newer && !update.swappable && update.latest && (
        <div className="load-notice live-refresh-notice advisory" role="status">
          <p>{t.live.updateManual(shortVersion(update.latest))}</p>
          {update.fix && (
            <p>
              <code>{update.fix}</code>
            </p>
          )}
        </div>
      )}
      {refreshError && (
        <div className="load-notice live-refresh-notice" role="status">
          <p className="form-error">{t.live.refreshError(refreshError)}</p>
          <button type="button" onClick={() => void reload()}>
            {t.live.retry}
          </button>
        </div>
      )}
      {updatedTo ? (
        <div className="load-notice live-refresh-notice advisory" role="status">
          <p>{t.live.panelUpdated(shortVersion(updatedTo))}</p>
          <button
            type="button"
            onClick={() => {
              if (activeRef.current) sessionStorage.setItem(STORAGE_KEY, activeRef.current)
              pageReload.run()
            }}
          >
            {t.live.reloadPage}
          </button>
        </div>
      ) : (
        newBinary && (
          <div
            className={`load-notice live-refresh-notice${restart.state === 'failed' ? '' : ' advisory'}`}
            role="status"
          >
            {restart.state === 'failed' ? (
              <p className="form-error">
                {restart.error === t.live.restartTimedOut ? restart.error : t.live.restartFailed(restart.error ?? '')}
              </p>
            ) : (
              <p>
                {restart.state === 'restarting'
                  ? t.live.restarting
                  : t.live.restartAvailable(shortVersion(installed.version), shortVersion(panel?.running.version ?? ''))}
              </p>
            )}
            {/* aria-disabled, não disabled: o botão fica focável enquanto reinicia (F-0302) */}
            <button type="button" onClick={doRestart} aria-disabled={restart.state === 'restarting'}>
              {t.live.restartPanel}
            </button>
            {/* durante o reinício não: apagaria o prazo que detecta a falha */}
            {restart.state !== 'restarting' && (
              <button type="button" onClick={postponeRestart}>
                {t.live.later}
              </button>
            )}
          </div>
        )
      )}
      {linkMissing && (
        <div className="load-notice live-refresh-notice" role="status">
          <p className="form-error">{t.live.linkMissing(linkMissing)}</p>
        </div>
      )}
      {children}
    </Ctx.Provider>
  )
}

// useLiveVersion devolve um número que muda quando o projeto ativo avança no
// backend ou quando o projeto ativo é trocado — as telas o põem nas deps do
// fetch para re-buscar ao vivo.
export function useLiveVersion(): number {
  return useContext(Ctx)?.version ?? 0
}

// useProjects expõe a lista, o ativo e o seletor para o Nav.
export function useProjects(): ProjectsState {
  const c = useContext(Ctx)
  const { t } = useT()
  if (!c) throw new Error(t.live.useProjectsError)
  return c
}
