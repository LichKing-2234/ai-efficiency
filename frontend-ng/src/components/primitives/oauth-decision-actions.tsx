import type { LucideIcon } from 'lucide-react'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { QuietActionButton } from '@/components/primitives/quiet-action-button'
import { SplitActions } from '@/components/primitives/split-actions'

export function OAuthDecisionActions({
  approveLabel,
  denyLabel,
  disabled,
  icon,
  onApprove,
  onDeny
}: {
  approveLabel: string
  denyLabel: string
  disabled: boolean
  icon: LucideIcon
  onApprove: () => void
  onDeny: () => void
}) {
  return (
    <SplitActions>
      <ButtonWithIcon icon={icon} disabled={disabled} onClick={onApprove}>
        {approveLabel}
      </ButtonWithIcon>
      <QuietActionButton disabled={disabled} onClick={onDeny}>
        {denyLabel}
      </QuietActionButton>
    </SplitActions>
  )
}
