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
  type ProjectInfo,
} from './api'
import { useT } from './i18n'

// Multi-projeto + atualização ao vivo (casos 0031/0032). O painel serve N
// projetos; este provider mantém a lista, o projeto ativo (global, em
// localStorage e sincronizado entre abas), o conjunto de projetos com mudanças
// pendentes (badge) e um contador de versão que as telas usam para re-buscar.

const STORAGE_KEY = 'cerne.activePid'

type ProjectsState = {
  projects: ProjectInfo[]
  activePid: string
  setActivePid: (pid: string) => void
  changed: Set<string>
  version: number
}

const Ctx = createContext<ProjectsState | null>(null)

export function LiveProvider({ children }: { children: ReactNode }) {
  const [projects, setProjects] = useState<ProjectInfo[]>([])
  const [activePid, setActivePidState] = useState('')
  const [changed, setChanged] = useState<Set<string>>(() => new Set())
  const [version, setVersion] = useState(0)
  const [ready, setReady] = useState(false)
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
    const saved = localStorage.getItem(STORAGE_KEY) ?? ''
    if (saved && has(saved)) return saved
    if (def && has(def)) return def
    return list[0]?.pid ?? ''
  }

  const reload = () =>
    fetchProjects()
      .then(({ projects, default: def }) => {
        setProjects(projects)
        apply(pick(projects, def))
        // Limpa badges de projetos que saíram do painel.
        setChanged((s) => new Set([...s].filter((p) => projects.some((x) => x.pid === p))))
        setReady(true)
      })
      .catch(() => setReady(true))

  useEffect(() => {
    void reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    return subscribeLive({
      onChange: (pid) => {
        if (pid === activeRef.current) setVersion((v) => v + 1)
        else setChanged((s) => new Set(s).add(pid))
      },
      onProjects: () => void reload(),
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Sincroniza o projeto ativo entre abas (outra aba gravou no localStorage).
  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY && e.newValue) {
        setChanged((s) => {
          const n = new Set(s)
          n.delete(e.newValue!)
          return n
        })
        apply(e.newValue)
      }
    }
    window.addEventListener('storage', onStorage)
    return () => window.removeEventListener('storage', onStorage)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const setActivePid = (pid: string) => {
    localStorage.setItem(STORAGE_KEY, pid)
    setChanged((s) => {
      const n = new Set(s)
      n.delete(pid)
      return n
    })
    apply(pid)
  }

  if (!ready) return <p className="status">{t.live.loading}</p>

  return (
    <Ctx.Provider value={{ projects, activePid, setActivePid, changed, version }}>
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
