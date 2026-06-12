import { useEffect, useState } from 'react'
import Kanban from './Kanban'
import FeaturePage from './FeaturePage'
import './app.css'

// Roteamento mínimo por hash: "#/" → kanban, "#/f/<id>" → página da feature.
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
  const match = /^#\/f\/(.+)$/.exec(hash)
  if (match) return <FeaturePage key={match[1]} id={decodeURIComponent(match[1])} />
  return <Kanban />
}
