import { useEffect, useState } from 'react'
import Kanban from './Kanban'
import FeaturePage from './FeaturePage'
import Nav from './Nav'
import Decisions, { DecisionDetail } from './Decisions'
import References, { ReferenceDetail } from './References'
import Search from './Search'
import Lint from './Lint'
import History from './History'
import './app.css'

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
    <>
      <Nav hash={hash} />
      <Routed hash={hash} />
    </>
  )
}

function Routed({ hash }: { hash: string }) {
  const feature = /^#\/f\/(.+)$/.exec(hash)
  if (feature) return <FeaturePage key={feature[1]} id={decodeURIComponent(feature[1])} />
  const decision = /^#\/d\/(.+)$/.exec(hash)
  if (decision) return <DecisionDetail key={decision[1]} id={decodeURIComponent(decision[1])} />
  const reference = /^#\/r\/(.+)$/.exec(hash)
  if (reference) return <ReferenceDetail key={reference[1]} id={decodeURIComponent(reference[1])} />
  if (hash === '#/decisions') return <Decisions />
  if (hash === '#/references') return <References />
  if (hash === '#/search') return <Search />
  if (hash === '#/lint') return <Lint />
  if (hash === '#/history') return <History />
  return <Kanban />
}
