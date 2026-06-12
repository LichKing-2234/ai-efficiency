import type * as React from 'react'
import type { LucideIcon } from 'lucide-react'
import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
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
    <Stack
      className={cn('rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-[14px]', className)}
      dataSlot='credential-key-panel'
      gap='compact'
    >
      <div className='font-semibold text-[10.5px] text-[var(--ink-3)] uppercase tracking-[0.06em]'>{label}</div>
      <ActionGroup
        align='start'
        className='min-w-0 rounded-[var(--r-md)] border border-border bg-[var(--surface)] px-[14px] py-[11px]'
        dataSlot='credential-key-row'
        fit
      >
        <Icon data-slot='credential-key-icon' className={ready ? 'text-[var(--ai)]' : 'text-[var(--ink-3)]'} />
        <span data-slot='credential-key-value' className={cn('mono min-w-0 flex-1 truncate text-[13px]', ready ? 'text-[var(--ai-deep)]' : 'text-[var(--ink-3)]')}>
          {value}
        </span>
        {actions}
      </ActionGroup>
      {footer ? (
        <ActionGroup align='start' className='mt-1.5' dataSlot='credential-key-footer' wrap>
          {footer}
        </ActionGroup>
      ) : null}
    </Stack>
  )
}
