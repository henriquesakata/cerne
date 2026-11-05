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
  setApiProject,
  subscribeLive,
  type ModelsChange,
  type ProjectInfo,
} from './api'
import { useT } from './i18n'

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
  const { t } = useT()

  // activeRef espelha o pid ativo para uso síncrono fora do ciclo de render
  // (callbacks de SSE e o reload da lista), evitando closures velhas.
  const activeRef = useRef('')

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
  const reload = () =>
    fetchProjects()
      .then(({ projects, default: def }) => {
        setProjects(projects)
        apply(pick(projects, def))
        // Limpa badges de projetos que saíram do painel.
        setChanged((s) => new Set([...s].filter((p) => projects.some((x) => x.pid === p))))
        setBootError(null)
        setReady(true)
      })
      .catch((e: unknown) => setBootError(e instanceof Error ? e.message : String(e)))

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
      },
      onProjects: () => void reload(),
      onModels: setModelsChange,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const setActivePid = (pid: string) => {
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
  if (!ready) return <p className="status">{t.live.loading}</p>
  if (projects.length === 0) return <p className="status">{t.live.noProjects}</p>

  return (
    <Ctx.Provider value={{ projects, activePid, setActivePid, changed, version, lastChange, modelsChange }}>
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
