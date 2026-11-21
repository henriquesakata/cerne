import { useT } from './i18n'

const KIND_NAMES: Record<string, string> = { github: 'GitHub', gitlab: 'GitLab', gitea: 'Gitea', jira: 'Jira', trello: 'Trello' }

// MirrorBadge (B-0063) marks an idea or a card linked to a tracker issue:
// "GitHub issue #4". Inside a card, which is a whole <a>, it is a plain span
// (a link inside a link is invalid HTML); the issue link is on the card's page.
export default function MirrorBadge({ mirror, link = false }: { mirror: { kind: string; id: string; url: string }; link?: boolean }) {
  const { t } = useT()
  const label = t.ideas.issueBadge(KIND_NAMES[mirror.kind] ?? mirror.kind, /^\d+$/.test(mirror.id) ? `#${mirror.id}` : mirror.id)
  if (!link) return <span className="badge badge-mirror">{label}</span>
  return (
    <a className="badge badge-mirror" href={mirror.url} target="_blank" rel="noreferrer">
      {label}
    </a>
  )
}
