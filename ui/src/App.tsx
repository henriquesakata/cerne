import { lazy, Suspense, useEffect, useState } from 'react'
import Kanban from './Kanban'
import Backlog from './Backlog'
import FeaturePage from './FeaturePage'
import Nav from './Nav'
import Decisions, { DecisionDetail } from './Decisions'
import References, { ReferenceDetail } from './References'
import Runbooks, { RunbookDetail } from './Runbooks'
import Notes, { NoteDetail } from './Notes'
import Conventions from './Conventions'
import Search from './Search'
import Palette from './Palette'
import Ask from './Ask'
import Wiki, { WikiPageView } from './Wiki'
import Lint from './Lint'
import Harness from './Harness'
import History from './History'
import Manage from './Manage'
import NewFeature from './NewFeature'
import StatsPage from './Stats'
import Activity from './Activity'
import Handoffs, { HandoffPage } from './Handoffs'
import Dashboard from './Dashboard'
import WarRoom, { WarRoomDetail } from './WarRoom'
import Channel from './Channel'
import Settings from './Settings'
import Pendings from './Pendings'
import IdeasPanel from './IdeasPanel'
import { fetchState } from './api'
import { LoadPending, useLiveLoad } from './liveLoad'
import { LiveProvider, useProjects } from './live'
import { NotificationsProvider } from './notifications'
import { useT } from './i18n'
import './app.css'

// Grafo e Mindmap carregam React Flow (pesado) sob demanda, como o Excalidraw.
const Graph = lazy(() => import('./Graph'))
const Mindmap = lazy(() => import('./Mindmap'))

// Roteamento mínimo por hash: lista de seções + rotas de detalhe
// (#/f/<id>, #/d/<id>, #/r/<id>). Rota desconhecida cai no kanban.
function useHash(): string {
  const [hash, setHash] = useState(window.location.hash)
  useEffect(() => {
    const onChange = () => setHash(window.location.hash)
    window.addEventListener('hashchange', onChange)
    return () => window.removeEventListener('hashchange', onChange)
  }, [])
  return hash
}

export default function App() {
  const hash = useHash()
  return (
    <LiveProvider>
      <NotificationsProvider>
        <Nav hash={hash} />
        {/* Global por design (F-0196): a palette vale em qualquer rota, e não
            pode depender de qual tela está montada. */}
        <Palette />
        <Routed hash={hash} />
      </NotificationsProvider>
    </LiveProvider>
  )
}

function Routed({ hash }: { hash: string }) {
  const { t } = useT()
  const { projects, activePid } = useProjects()
  // graph.enabled / mindmap.enabled = false: a rota também morre — cai no
  // dashboard (F-0315, F-0187).
  const info = projects.find((p) => p.pid === activePid)
  // Tab title follows the active project's name (F-0387); route-independent.
  const name = info?.name?.trim()
  useEffect(() => {
    document.title = name ? `cerne - ${name}` : 'cerne'
  }, [name])
  const graphOn = info?.graph !== false
  const mindmapOn = info?.mindmap !== false
  // A deep link to a tab turned off lands on the dashboard: the hash says so
  // too, so the nav marks Dashboard and Back skips the dead route (F-0315, C15).
  const gated = (hash === '#/graph' && !graphOn) || (hash === '#/mindmap' && !mindmapOn)
  useEffect(() => {
    if (gated) window.location.replace('#/')
  }, [gated])
  const feature = /^#\/f\/(.+)$/.exec(hash)
  if (feature) return <FeaturePage key={feature[1]} id={decodeURIComponent(feature[1])} />
  const decision = /^#\/d\/(.+)$/.exec(hash)
  if (decision) return <DecisionDetail key={decision[1]} id={decodeURIComponent(decision[1])} />
  const reference = /^#\/r\/(.+)$/.exec(hash)
  if (reference) return <ReferenceDetail key={reference[1]} id={decodeURIComponent(reference[1])} />
  const runbook = /^#\/rb\/(.+)$/.exec(hash)
  if (runbook) return <RunbookDetail key={runbook[1]} id={decodeURIComponent(runbook[1])} />
  const note = /^#\/n\/(.+)$/.exec(hash)
  if (note) return <NoteDetail key={note[1]} id={decodeURIComponent(note[1])} />
  if (hash === '#/rooms/channel') return <Channel />
  const room = /^#\/rooms\/(.+)$/.exec(hash)
  if (room) return <WarRoomDetail key={room[1]} id={decodeURIComponent(room[1])} />
  const handoff = /^#\/handoffs\/(.+)$/.exec(hash)
  if (handoff) return <HandoffPage key={handoff[1]} id={decodeURIComponent(handoff[1])} />
  const wikiPage = /^#\/wiki\/(.+)$/.exec(hash)
  if (wikiPage) return <WikiPageView key={wikiPage[1]} rel={decodeURIComponent(wikiPage[1])} />
  if (hash === '#/decisions') return <Decisions />
  if (hash === '#/references') return <References />
  if (hash === '#/runbooks') return <Runbooks />
  if (hash === '#/notes') return <Notes />
  if (hash === '#/conventions') return <Conventions />
  if (hash === '#/wiki') return <Wiki />
  if (hash === '#/graph' && graphOn)
    return (
      <Suspense fallback={<p className="status">{t.graph.loadingChunk}</p>}>
        <Graph />
      </Suspense>
    )
  if (hash === '#/mindmap' && mindmapOn)
    return (
      <Suspense fallback={<p className="status">{t.mindmap.loadingChunk}</p>}>
        <Mindmap />
      </Suspense>
    )
  if (hash === '#/ask') return <Ask />
  // Avaliada antes da rota exata: #/search/<termo> vem da palette, com o termo
  // já aplicado (F-0196); #/search puro segue inerte até o submit.
  const searchQuery = /^#\/search\/(.+)$/.exec(hash)
  if (searchQuery) return <Search key={searchQuery[1]} initialQuery={decodeURIComponent(searchQuery[1])} />
  if (hash === '#/search') return <Search />
  if (hash === '#/lint') return <Lint />
  if (hash === '#/harness') return <Harness />
  if (hash === '#/history') return <History />
  if (hash === '#/stats') return <StatsPage />
  if (hash === '#/activity') return <Activity />
  if (hash === '#/handoffs' || hash.startsWith('#/handoffs?')) return <Handoffs hash={hash} />
  if (hash === '#/manage') return <Manage />
  if (hash === '#/settings') return <Settings />
  if (hash === '#/new') return <NewFeature />
  if (hash === '#/pendings' || hash.startsWith('#/pendings?')) return <Pendings hash={hash} />
  if (hash === '#/ideas') return <IdeasPage />
  if (hash === '#/kanban') return <Kanban />
  if (hash === '#/backlog') return <Backlog />
  if (hash === '#/rooms') return <WarRoom />
  // Tela inicial (F-0132): o dashboard responde "como está o projeto?".
  return <Dashboard />
}

// IdeasPage (F-0334 crit. 5): a segunda porta do painel de ideias da F-0312,
// com os módulos do projeto para o Promover.
// O painel só monta com os módulos na mão: sem eles, o Promover num projeto de
// 2+ módulos sairia sem --module e seria recusado (crítica C16).
function IdeasPage() {
  const { t } = useT()
  const load = useLiveLoad(fetchState)
  // Here no candidate list sits beside the panel, so the promotion is said in a
  // live region, with the way to the new candidate (crítica C5).
  const [promoted, setPromoted] = useState(false)
  return (
    <main className="page">
      {/* Every screen has an h1; the panel already titles itself (its h2). */}
      <h1 className="sr-only">{t.nav.ideas}</h1>
      {load.data === null ? (
        <LoadPending load={load} message={t.kanban.loadError} />
      ) : (
        <>
          <p className="ideas-promoted" role="status">
            {promoted && (
              <>
                {t.ideas.promotedNotice} <a href="#/backlog">{t.ideas.openBacklog}</a>
              </>
            )}
          </p>
          <IdeasPanel modules={load.data.project.modules ?? []} onPromoted={() => setPromoted(true)} />
        </>
      )}
    </main>
  )
}
