import { ConfirmAction } from '@/components/primitives/confirm-action'
import { Button } from '@/components/ui/button'

export function ConfirmActionButton({
  cancelLabel,
  confirmLabel,
  description,
  disabled,
  label,
  onConfirm,
  title
}: {
  cancelLabel: string
  confirmLabel: string
  description: string
  disabled?: boolean
  label: string
  onConfirm: () => void
  title: string
}) {
  return (
    <ConfirmAction
      cancelLabel={cancelLabel}
      confirmLabel={confirmLabel}
      description={description}
      disabled={disabled}
      onConfirm={onConfirm}
      title={title}
      trigger={<Button size='sm' variant='ghost' disabled={disabled}>{label}</Button>}
    />
  )
}
