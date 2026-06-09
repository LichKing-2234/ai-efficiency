import type * as React from 'react'
import { cn } from '@/lib/utils'

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
  trailing
}: {
  className?: string
  description?: React.ReactNode
  href: string
  icon?: React.ReactNode
  label: React.ReactNode
  trailing?: React.ReactNode
}) {
  return (
    <a
      className={cn('flex min-w-0 items-center gap-2 rounded-[var(--r-md)] border border-border bg-card px-3 py-2 text-foreground transition hover:border-[var(--ai-line)] hover:bg-[var(--ai-soft)] hover:text-[var(--ai-deep)]', className)}
      data-slot='linked-record-item'
      href={href}
      rel='noreferrer'
      target='_blank'
    >
      {icon ? <span className='grid size-4 shrink-0 place-items-center text-[var(--ai)]'>{icon}</span> : null}
      <span className='min-w-0 flex-1'>
        <span className='block truncate font-medium text-sm'>{label}</span>
        {description ? <span className='mt-1 block truncate text-muted-foreground text-xs'>{description}</span> : null}
      </span>
      {trailing ? <span className='shrink-0 text-muted-foreground'>{trailing}</span> : null}
    </a>
  )
}
