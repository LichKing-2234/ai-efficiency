import { AppAlert } from '@/components/primitives/app-alert'
import { SubmitCancelActions } from '@/components/primitives/submit-cancel-actions'

export function ManagedFormFooter({
  cancelLabel,
  errors = [],
  submitDisabled,
  submitLabel,
  onCancel,
  onSubmit
}: {
  cancelLabel: string
  errors?: Array<string | undefined>
  submitDisabled?: boolean
  submitLabel: string
  onCancel: () => void
  onSubmit: () => void
}) {
  return (
    <>
      {errors.filter((message): message is string => !!message).map((message) => (
        <AppAlert key={message} tone='error' title={message} />
      ))}
      <SubmitCancelActions
        cancelLabel={cancelLabel}
        submitDisabled={submitDisabled}
        submitLabel={submitLabel}
        onCancel={onCancel}
        onSubmit={onSubmit}
      />
    </>
  )
}
