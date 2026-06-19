import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from 'react'
import { subscribeChanges } from './api'

// Atualização ao vivo (case 0031): um contador que incrementa toda vez que o
// backend avança (sinal SSE de /api/stream). As telas incluem este número nas
// dependências do seu fetch para re-buscar sozinhas — sem F5.
const LiveVersionContext = createContext(0)

// LiveProvider mantém uma única conexão SSE para todo o app e expõe o contador
// de versão pelo contexto. O EventSource reconecta sozinho; ao reconectar, o
// servidor reenvia o SHA atual, então nenhuma mudança se perde num drop curto.
export function LiveProvider({ children }: { children: ReactNode }) {
  const [version, setVersion] = useState(0)
  useEffect(() => subscribeChanges(() => setVersion((v) => v + 1)), [])
  return (
    <LiveVersionContext.Provider value={version}>
      {children}
    </LiveVersionContext.Provider>
  )
}

// useLiveVersion devolve um número que muda a cada alteração do backend.
// Inclua-o nas dependências do useEffect de fetch para re-buscar ao vivo.
export function useLiveVersion(): number {
  return useContext(LiveVersionContext)
}
