import { ActionGroup } from '@/components/primitives/action-group'
import { Button } from '@/components/ui/button'

export function InlineConfirmActions({
  cancelLabel,
  confirmLabel,
  confirmVariant = 'outline',
  disabled,
  onCancel,
  onConfirm,
  push = false,
  wrap = false
}: {
  cancelLabel: string
  confirmLabel: string
  confirmVariant?: 'destructive' | 'outline'
  disabled?: boolean
  onCancel: () => void
  onConfirm: () => void
  push?: boolean
  wrap?: boolean
}) {
  return (
    <ActionGroup dataSlot='inline-confirm-actions' push={push} wrap={wrap}>
      <Button type='button' variant={confirmVariant} onClick={onConfirm} disabled={disabled}>
        {confirmLabel}
      </Button>
      <Button type='button' variant='ghost' onClick={onCancel}>
        {cancelLabel}
      </Button>
    </ActionGroup>
  )
}
