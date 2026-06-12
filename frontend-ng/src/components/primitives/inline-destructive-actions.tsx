import { ActionGroup } from '@/components/primitives/action-group'
import { Button } from '@/components/ui/button'
import { InlineConfirmActions } from './inline-confirm-actions'

export function InlineDestructiveActions({
  armed,
  cancelLabel,
  confirmLabel,
  confirmPending = false,
  onArm,
  onCancel,
  onConfirm,
  triggerLabel
}: {
  armed: boolean
  cancelLabel: string
  confirmLabel: string
  confirmPending?: boolean
  onArm: () => void
  onCancel: () => void
  onConfirm: () => void
  triggerLabel: string
}) {
  if (armed) {
    return (
      <InlineConfirmActions
        cancelLabel={cancelLabel}
        confirmLabel={confirmLabel}
        confirmVariant='destructive'
        disabled={confirmPending}
        onCancel={onCancel}
        onConfirm={onConfirm}
        push
        wrap
      />
    )
  }

  return (
    <ActionGroup push>
      <Button type='button' variant='ghost' onClick={onArm}>
        {triggerLabel}
      </Button>
    </ActionGroup>
  )
}
