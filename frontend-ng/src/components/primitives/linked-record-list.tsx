import type * as React from 'react'
import { cn } from '@/lib/utils'

const linkedRecordDescriptionClass = 'mt-1 block truncate text-muted-foreground text-xs'

function LinkedRecordDescription({ children }: { children: React.ReactNode }) {
  return (
    <span className={linkedRecordDescriptionClass} data-slot='linked-record-description'>
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
    <div className={cn('flex flex-col gap-2', className)} data-slot='linked-record-list'>
      {children}
    </div>
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
        variant === 'card' && 'border border-border bg-card px-3 py-2 hover:border-[var(--ai-line)] hover:bg-[var(--ai-soft)]',
        variant === 'plain' && 'border-0 bg-transparent p-0 hover:bg-transparent',
        className
      )}
      data-slot='linked-record-item'
      href={href}
      rel='noreferrer'
      target='_blank'
    >
      {icon ? <span className='grid size-4 shrink-0 place-items-center text-[var(--ai)]'>{icon}</span> : null}
      <span className='min-w-0 flex-1'>
        <span className='block truncate font-medium text-sm'>{label}</span>
        {description ? <LinkedRecordDescription>{description}</LinkedRecordDescription> : null}
      </span>
      {trailing ? <span className='shrink-0 text-muted-foreground'>{trailing}</span> : null}
    </a>
  )
}
