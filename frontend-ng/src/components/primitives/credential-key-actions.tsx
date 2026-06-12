import { Clipboard } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function CredentialKeyActions({
  copyLabel,
  onCopy,
  onToggleReveal,
  revealed,
  revealLabel,
  hideLabel
}: {
  copyLabel: string
  onCopy: () => void
  onToggleReveal: () => void
  revealed: boolean
  revealLabel: string
  hideLabel: string
}) {
  return (
    <>
      <Button size='sm' type='button' variant='ghost' onClick={onToggleReveal}>
        {revealed ? hideLabel : revealLabel}
      </Button>
      <Button aria-label={copyLabel} size='icon-sm' type='button' variant='ghost' onClick={onCopy}>
        <Clipboard />
      </Button>
    </>
  )
}
