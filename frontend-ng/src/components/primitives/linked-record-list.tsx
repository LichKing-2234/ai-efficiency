import type * as React from 'react'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

function LinkedRecordDescription({ children }: { children: React.ReactNode }) {
  return (
    <span className='block truncate text-[11px] text-[var(--ink-4)]' data-slot='linked-record-description'>
      {children}
    </span>
  )
}

export function LinkedRecordList({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <Stack className={className} dataSlot='linked-record-list' gap='compact'>
      {children}
    </Stack>
  )
}

export function LinkedRecordItem({
  className,
  description,
  href,
  icon,
  label,
  trailing,
  variant = 'card'
}: {
  className?: string
  description?: React.ReactNode
  href: string
  icon?: React.ReactNode
  label: React.ReactNode
  trailing?: React.ReactNode
  variant?: 'card' | 'plain'
}) {
  return (
    <a
      className={cn(
        'flex min-w-0 items-center gap-2 rounded-[var(--r-md)] text-foreground transition hover:text-[var(--ai-deep)]',
        variant === 'card' && 'border border-border bg-card px-[12px] py-[9px] hover:border-[var(--ai-line)] hover:bg-[var(--ai-soft)]',
        variant === 'plain' && 'border-0 bg-transparent p-0 hover:bg-transparent',
        className
      )}
      data-slot='linked-record-item'
      href={href}
      rel='noreferrer'
      target='_blank'
    >
      {icon ? <span className='grid size-4 shrink-0 place-items-center text-[var(--ai)]'>{icon}</span> : null}
      <Stack className='min-w-0 flex-1' dataSlot='linked-record-content' gap='none'>
        <span className='block truncate font-medium text-[12.5px]'>{label}</span>
        {description ? <LinkedRecordDescription>{description}</LinkedRecordDescription> : null}
      </Stack>
      {trailing ? <span className='shrink-0 text-[11px] text-[var(--ink-3)]'>{trailing}</span> : null}
    </a>
  )
}
