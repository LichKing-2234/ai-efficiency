import { CheckIcon, ClipboardIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'

export function commandStepDisplayText(command: string) {
  return command ? `$ ${command}` : '$'
}

export function commandStepClipboardText(command: string) {
  return command
}

export function CommandStep({
  command,
  copiedMessage,
  copyLabel,
  disabled,
  label,
  step
}: {
  command: string
  copiedMessage: string
  copyLabel?: string
  disabled?: boolean
  label?: string
  step?: number
}) {
  const [copied, setCopied] = useState(false)

  return (
    <div className='flex min-w-0 items-center gap-3'>
      {step ? (
        <div className='grid size-5 shrink-0 place-items-center rounded-full bg-[var(--ai-soft)] font-bold text-[11px] text-[var(--ai-deep)] tnum'>
          {step}
        </div>
      ) : null}
      <button
        className={cn(
          'flex min-w-0 flex-1 items-center gap-3 rounded-[var(--r-sm)] border border-border bg-[var(--surface-inset)] px-3 py-2 text-left text-xs transition hover:border-[var(--line-strong)] hover:bg-card disabled:cursor-not-allowed disabled:opacity-60'
        )}
        disabled={disabled}
        onClick={() => {
          const text = commandStepClipboardText(command)
          if (!text) return
          void navigator.clipboard?.writeText(text)
          setCopied(true)
          window.setTimeout(() => setCopied(false), 1200)
          toast.success(copiedMessage)
        }}
      >
        {label ? <span className='shrink-0 font-semibold text-muted-foreground'>{label}</span> : null}
        <span className='mono min-w-0 flex-1 truncate text-[var(--ai-deep)]'>{commandStepDisplayText(command)}</span>
        <span className='inline-flex shrink-0 items-center gap-1 text-muted-foreground'>
          {copied ? <CheckIcon data-icon='inline-start' /> : <ClipboardIcon data-icon='inline-start' />}
          {copyLabel}
        </span>
      </button>
    </div>
  )
}
