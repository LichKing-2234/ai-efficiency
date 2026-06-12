import { Clipboard } from 'lucide-react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Button } from '@/components/ui/button'

export function AdminSecretActions({
  copyDisabled = false,
  copyEncryptedLabel,
  revealDisabled = false,
  revealLabel,
  onCopyEncrypted,
  onReveal
}: {
  copyDisabled?: boolean
  copyEncryptedLabel: string
  revealDisabled?: boolean
  revealLabel: string
  onCopyEncrypted: () => void
  onReveal: () => void
}) {
  return (
    <ActionGroup className='gap-1' fit wrap dataSlot='admin-secret-actions'>
      <Button
        aria-label={copyEncryptedLabel}
        disabled={copyDisabled}
        size='icon-sm'
        type='button'
        variant='ghost'
        onClick={onCopyEncrypted}
      >
        <Clipboard />
      </Button>
      <Button
        disabled={revealDisabled}
        size='sm'
        type='button'
        variant='outline'
        onClick={onReveal}
      >
        {revealLabel}
      </Button>
    </ActionGroup>
  )
}
