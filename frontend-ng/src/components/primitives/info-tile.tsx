import type * as React from 'react'
import { cn } from '@/lib/utils'

export function InfoTile({
  label,
  value,
  accent = false,
  compact = false,
  mono = false,
  numeric = false,
  className
}: {
  label: React.ReactNode
  value: React.ReactNode
  accent?: boolean | 'ai'
  compact?: boolean
  mono?: boolean
  numeric?: boolean
  className?: string
}) {
  const aiAccent = accent === 'ai'
  const positiveAccent = accent === true

  return (
    <div
      data-slot='info-tile'
      className={cn(
        'rounded-[var(--r-md)] border bg-[var(--surface-inset)] p-3',
        aiAccent ? 'border-[var(--ai-line)] bg-[var(--ai-soft)]' : 'border-border',
        className
      )}
    >
      <div className={cn('font-semibold text-xs', compact ? 'text-muted-foreground' : 'text-muted-foreground uppercase', aiAccent && 'text-[var(--ai-deep)]')}>{label}</div>
      <div
        className={cn(
          'mt-1 break-all font-semibold text-sm',
          numeric && 'tnum',
          compact && 'text-[18px]',
          positiveAccent && 'text-[var(--pos)]',
          aiAccent && 'text-[var(--ai-deep)]',
          mono && 'mono truncate'
        )}
      >
        {value}
      </div>
    </div>
  )
}
