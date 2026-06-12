import type * as React from 'react'
import { cn } from '@/lib/utils'

export function AppBrand({
  className,
  compact = false,
  mark,
  subtitle,
  title
}: {
  className?: string
  compact?: boolean
  mark: React.ReactNode
  subtitle?: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <div
      className={cn('flex min-w-0 items-center font-semibold', compact && 'gap-[10px]', !compact && 'gap-2', className)}
      data-slot='app-brand'
    >
      <span
        className={cn(
          'grid shrink-0 place-items-center bg-[linear-gradient(135deg,var(--ai-bright),var(--ai-deep))] text-primary-foreground',
          compact ? 'size-8 rounded-[8px] text-[15px]' : 'size-7 rounded-[var(--r-sm)] text-xs'
        )}
        data-slot='app-brand-mark'
      >
        {mark}
      </span>
      <span className='min-w-0'>
        <span
          className={cn('block truncate tracking-[0] text-foreground', compact ? 'text-[13.5px] leading-[1.05] font-[650]' : 'text-[13.5px]')}
          data-slot='app-brand-title'
        >
          {title}
        </span>
        {subtitle ? (
          <span
            className={cn('block font-mono text-[10px] text-[var(--ink-4)]', compact ? 'mt-[2px] tracking-[0.02em]' : '')}
            data-slot='app-brand-subtitle'
          >
            {subtitle}
          </span>
        ) : null}
      </span>
    </div>
  )
}
