import type { Feature } from './api'
import { useT } from './i18n'

// GateBadges (F-0515) mark a card that crossed the approve or the accept gate
// with no human act ([gates] approve/accept = "auto"), on the kanban card and
// the feature page. The acceptance on merge (F-0488) keeps its own label.
export default function GateBadges({ feature }: { feature: Pick<Feature, 'approved_via' | 'accepted_via'> }) {
  const { t } = useT()
  return (
    <>
      {feature.approved_via === 'auto' && <span className="badge badge-auto">{t.kanban.approvedAuto}</span>}
      {feature.accepted_via === 'auto' && <span className="badge badge-auto">{t.kanban.acceptedAuto}</span>}
    </>
  )
}
