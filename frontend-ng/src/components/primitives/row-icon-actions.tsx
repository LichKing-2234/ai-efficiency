import type { LucideIcon } from 'lucide-react'
import { ActionGroup } from '@/components/primitives/action-group'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { Button } from '@/components/ui/button'

export function RowIconActions({
  cancelLabel,
  deleteDescription,
  deleteDisabled = false,
  deleteIcon: DeleteIcon,
  deleteLabel,
  deleteTitle,
  editDisabled = false,
  editIcon: EditIcon,
  editLabel,
  onDelete,
  onEdit
}: {
  cancelLabel: string
  deleteDescription: string
  deleteDisabled?: boolean
  deleteIcon: LucideIcon
  deleteLabel: string
  deleteTitle: string
  editDisabled?: boolean
  editIcon: LucideIcon
  editLabel: string
  onDelete: () => void
  onEdit: () => void
}) {
  return (
    <ActionGroup className='gap-1' dataSlot='row-icon-actions' fit>
      <Button aria-label={editLabel} title={editLabel} size='icon-sm' type='button' variant='ghost' disabled={editDisabled} onClick={onEdit}>
        <EditIcon data-icon='icon' aria-hidden='true' />
      </Button>
      <ConfirmAction
        trigger={
          <Button aria-label={deleteLabel} title={deleteLabel} size='icon-sm' type='button' variant='ghost' disabled={deleteDisabled}>
            <DeleteIcon data-icon='icon' aria-hidden='true' />
          </Button>
        }
        title={deleteTitle}
        description={deleteDescription}
        confirmLabel={deleteLabel}
        cancelLabel={cancelLabel}
        onConfirm={onDelete}
        disabled={deleteDisabled}
      />
    </ActionGroup>
  )
}
