import { useEffect, useState } from 'react'

// Tema claro/escuro (F-0126): o valor efetivo vive no atributo data-theme do
// <html>, setado antes do primeiro paint por um script inline no index.html.
// Aqui ficam a leitura/gravação da escolha e o hook que o toggle da nav usa.
export type Theme = 'light' | 'dark'

const STORAGE_KEY = 'cerne-theme'

export function getTheme(): Theme {
  return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light'
}

export function setTheme(t: Theme) {
  document.documentElement.dataset.theme = t
  localStorage.setItem(STORAGE_KEY, t)
}

export function useTheme(): [Theme, (t: Theme) => void] {
  const [theme, set] = useState<Theme>(getTheme)
  useEffect(() => {
    setTheme(theme)
  }, [theme])
  return [theme, set]
}
