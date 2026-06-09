import type * as React from 'react'
import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

export function CredentialKeyPanel({
  label,
  value,
  ready = false,
  icon: Icon,
  actions,
  footer,
  className
}: {
  label: React.ReactNode
  value: React.ReactNode
  ready?: boolean
  icon: LucideIcon
  actions?: React.ReactNode
  footer?: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot='credential-key-panel' className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-4', className)}>
      <div className='mb-2 font-semibold text-muted-foreground text-xs uppercase'>{label}</div>
      <div className='flex min-w-0 items-center gap-3 rounded-[var(--r-md)] border border-border bg-card px-3 py-3'>
        <Icon data-slot='credential-key-icon' className={ready ? 'text-[var(--ai)]' : 'text-muted-foreground'} />
        <span data-slot='credential-key-value' className={cn('mono min-w-0 flex-1 truncate text-sm', ready ? 'text-[var(--ai-deep)]' : 'text-muted-foreground')}>
          {value}
        </span>
        {actions}
      </div>
      {footer ? <div className='mt-3 flex flex-wrap gap-2'>{footer}</div> : null}
    </div>
  )
}
