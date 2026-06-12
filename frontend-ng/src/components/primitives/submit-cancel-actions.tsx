import { Button } from '@/components/ui/button'
import { FormActions } from '@/components/primitives/form-actions'

export function SubmitCancelActions({
  cancelDisabled = false,
  cancelLabel,
  onCancel,
  onSubmit,
  submitDisabled = false,
  submitLabel
}: {
  cancelDisabled?: boolean
  cancelLabel: string
  onCancel: () => void
  onSubmit: () => void
  submitDisabled?: boolean
  submitLabel: string
}) {
  return (
    <FormActions>
      <Button variant='outline' onClick={onCancel} disabled={cancelDisabled}>
        {cancelLabel}
      </Button>
      <Button disabled={submitDisabled} onClick={onSubmit}>
        {submitLabel}
      </Button>
    </FormActions>
  )
}
