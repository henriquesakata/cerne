import Markdown from './Markdown'
import { useT } from './i18n'

// Seção de errata (F-0163): correções aditivas de uma entidade concluída,
// sempre visíveis fora das abas — a errata corrige o registro inteiro, não um
// doc específico. Vazio → nada (a maioria das entidades não tem errata).
export default function ErrataSection({ content }: { content?: string }) {
  const { t } = useT()
  if (!content) return null
  return (
    <section className="errata-section">
      <h2 className="errata-title">{t.errata.title}</h2>
      <Markdown>{content}</Markdown>
    </section>
  )
}
