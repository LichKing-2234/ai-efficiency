import type * as React from 'react'
import { cn } from '@/lib/utils'

const infoTileGridColumns = {
  2: 'min-[920px]:grid-cols-2',
  3: 'min-[920px]:grid-cols-3',
  4: 'min-[920px]:grid-cols-4'
} as const

export function InfoTileGrid({
  children,
  className,
  columns = 3
}: {
  children: React.ReactNode
  className?: string
  columns?: keyof typeof infoTileGridColumns
}) {
  return (
    <div data-slot='info-tile-grid' className={cn('grid gap-[10px]', infoTileGridColumns[columns], className)}>
      {children}
    </div>
  )
}

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
        'rounded-[var(--r-md)] border bg-[var(--surface-inset)] px-[12px] py-[11px]',
        aiAccent ? 'border-[var(--ai-line)] bg-[var(--ai-soft)]' : 'border-border',
        className
      )}
    >
      <div className={cn('font-medium text-[11px]', compact ? 'text-[var(--ink-3)]' : 'text-[var(--ink-3)] uppercase', aiAccent && 'text-[var(--ai-deep)]')}>{label}</div>
      <div
        className={cn(
          'mt-[3px] break-all font-[680] text-[18px]',
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
