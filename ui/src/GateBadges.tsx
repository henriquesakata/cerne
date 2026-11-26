import type { Feature } from './api'
import { useT } from './i18n'

// GateBadges (F-0515) mark a card that crossed the approve or the accept gate
// with no human act ([gates] approve/accept = "auto"), on the kanban card and
// the feature page. The acceptance on merge (F-0488) keeps its own label. A
// card the risk gate accepted (F-0516) shows its measurements on hover. The
// owner's accept mode (F-0518) shows too: passing alone is the warning. A card
// the risk gate holds (F-0521) says why: the reasons are in the title and the
// accessible name.
export default function GateBadges({ feature }: { feature: Pick<Feature, 'state' | 'approved_via' | 'accepted_via' | 'accepted_risk' | 'accept_mode' | 'held_risk'> }) {
  const { t } = useT()
  return (
    <>
      {feature.accept_mode === 'human' && <span className="badge">{t.riskGate.modes.human}</span>}
      {feature.accept_mode === 'auto' && <span className="badge badge-auto">{t.riskGate.modes.auto}</span>}
      {feature.approved_via === 'auto' && <span className="badge badge-auto">{t.kanban.approvedAuto}</span>}
      {feature.accepted_via === 'auto' && <span className="badge badge-auto">{t.kanban.acceptedAuto}</span>}
      {feature.accepted_via === 'risk' && (
        <span className="badge badge-risk" title={feature.accepted_risk}>
          {t.kanban.acceptedRisk}
        </span>
      )}
      {feature.state === 'implemented' && feature.held_risk && feature.held_risk.length > 0 && (
        <span
          className="badge badge-held"
          role="note"
          title={feature.held_risk.join('\n')}
          aria-label={`${t.kanban.heldRisk}: ${feature.held_risk.join('; ')}`}
        >
          {t.kanban.heldRisk}
        </span>
      )}
    </>
  )
}
