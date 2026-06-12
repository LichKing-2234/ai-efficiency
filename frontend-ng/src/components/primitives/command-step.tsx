import { CheckIcon, ClipboardIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'
import { Stack } from './stack'

export function commandStepDisplayText(command: string) {
  return command ? `$ ${command}` : '$'
}

export function commandStepClipboardText(command: string) {
  return command
}

export function commandStepAriaLabel(command: string, label?: string) {
  if (label && command) return `${label}: ${command}`
  return label || command || 'command'
}

export function CommandStep({
  command,
  copiedMessage,
  disabled,
  label,
  step
}: {
  command: string
  copiedMessage: string
  disabled?: boolean
  label?: string
  step?: number
}) {
  const [copied, setCopied] = useState(false)

  return (
    <ActionGroup align='start' className='min-w-0 gap-[10px]' dataSlot='command-step' fit>
      {step ? (
        <Stack className='grid size-5 shrink-0 place-items-center rounded-full bg-[var(--ai-soft)] font-bold text-[11px] text-[var(--ai-deep)] tnum' dataSlot='command-step-index' gap='none'>
          {step}
        </Stack>
      ) : null}
      <button
        className={cn(
          'flex min-w-0 flex-1 items-center gap-2 rounded-[var(--r-sm)] border border-border bg-[var(--surface-inset)] px-[10px] py-[8px] text-left text-[12px] transition hover:border-[var(--line-strong)] hover:bg-card disabled:cursor-not-allowed disabled:opacity-60'
        )}
        disabled={disabled}
        aria-label={commandStepAriaLabel(command, label)}
        title={label}
        onClick={() => {
          const text = commandStepClipboardText(command)
          if (!text) return
          void navigator.clipboard?.writeText(text)
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1200)
          toast.success(copiedMessage)
        }}
      >
        <span className='mono min-w-0 flex-1 truncate text-[11.5px] text-[var(--ai-deep)]'>{commandStepDisplayText(command)}</span>
        <ActionGroup className='gap-1 text-[var(--ink-4)]' dataSlot='command-step-copy' fit>
          {copied ? <CheckIcon data-icon='inline-start' /> : <ClipboardIcon data-icon='inline-start' />}
        </ActionGroup>
      </button>
    </ActionGroup>
  )
}
