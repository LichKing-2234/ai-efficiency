import type * as React from 'react'
import { InlineDestructiveActions } from '@/components/primitives/inline-destructive-actions'
import { SplitActions } from '@/components/primitives/split-actions'

export function DetailDrawerActions({
  actions,
  armed,
  cancelLabel,
  confirmLabel,
  confirmPending = false,
  onArm,
  onCancel,
  onConfirm,
  triggerLabel
}: {
  actions: React.ReactNode
  armed: boolean
  cancelLabel: string
  confirmLabel: string
  confirmPending?: boolean
  onArm: () => void
  onCancel: () => void
  onConfirm: () => void
  triggerLabel: string
}) {
  return (
    <>
      <SplitActions>{actions}</SplitActions>
      <InlineDestructiveActions
        armed={armed}
        cancelLabel={cancelLabel}
        confirmLabel={confirmLabel}
        confirmPending={confirmPending}
        triggerLabel={triggerLabel}
        onArm={onArm}
        onCancel={onCancel}
        onConfirm={onConfirm}
      />
    </>
  )
}
