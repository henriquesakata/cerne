import { lazy, Suspense, useEffect, useState } from 'react'
import Kanban from './Kanban'
import FeaturePage from './FeaturePage'
import Nav from './Nav'
import Decisions, { DecisionDetail } from './Decisions'
import References, { ReferenceDetail } from './References'
import Runbooks, { RunbookDetail } from './Runbooks'
import Notes, { NoteDetail } from './Notes'
import Conventions from './Conventions'
import Search from './Search'
import Ask from './Ask'
import Wiki, { WikiPageView } from './Wiki'
import Lint from './Lint'
import History from './History'
import Manage from './Manage'
import NewFeature from './NewFeature'
import StatsPage from './Stats'
import Activity from './Activity'
import Dashboard from './Dashboard'
import { LiveProvider } from './live'
import { NotificationsProvider } from './notifications'
import { useT } from './i18n'
import './app.css'

// Grafo carrega React Flow (pesado) sob demanda, como o Excalidraw.
const Graph = lazy(() => import('./Graph'))

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
        <Routed hash={hash} />
      </NotificationsProvider>
    </LiveProvider>
  )
}

function Routed({ hash }: { hash: string }) {
  const { t } = useT()
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
  const wikiPage = /^#\/wiki\/(.+)$/.exec(hash)
  if (wikiPage) return <WikiPageView key={wikiPage[1]} rel={decodeURIComponent(wikiPage[1])} />
  if (hash === '#/decisions') return <Decisions />
  if (hash === '#/references') return <References />
  if (hash === '#/runbooks') return <Runbooks />
  if (hash === '#/notes') return <Notes />
  if (hash === '#/conventions') return <Conventions />
  if (hash === '#/wiki') return <Wiki />
  if (hash === '#/graph')
    return (
      <Suspense fallback={<p className="status">{t.graph.loadingChunk}</p>}>
        <Graph />
      </Suspense>
    )
  if (hash === '#/ask') return <Ask />
  if (hash === '#/search') return <Search />
  if (hash === '#/lint') return <Lint />
  if (hash === '#/history') return <History />
  if (hash === '#/stats') return <StatsPage />
  if (hash === '#/activity') return <Activity />
  if (hash === '#/manage') return <Manage />
  if (hash === '#/new') return <NewFeature />
  if (hash === '#/kanban') return <Kanban />
  // Tela inicial (F-0132): o dashboard responde "como está o projeto?".
  return <Dashboard />
}
