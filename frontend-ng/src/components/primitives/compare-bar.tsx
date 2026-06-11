import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { cn } from '@/lib/utils'

export function CompareBar({
  color,
  labelClassName,
  label,
  max,
  value,
  valueLabel
}: {
  color: string
  label: React.ReactNode
  labelClassName?: string
  max: number
  value: number
  valueLabel: React.ReactNode
}) {
  const ratio = max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0

  return (
    <CardContentStack gap='compact'>
      <div className='flex items-center justify-between gap-3 text-[12px]'>
        <span className={cn('font-medium text-[var(--ink-2)]', labelClassName)}>{label}</span>
        <span className='tnum font-semibold text-[12px] text-[var(--ink)]'>{valueLabel}</span>
      </div>
      <div className='h-2.5 overflow-hidden rounded-[var(--r-full)] bg-[var(--surface-inset)]'>
        <div className='h-full rounded-[var(--r-full)] transition-[width] duration-700 ease-[var(--ease-out)]' style={{ width: `${ratio}%`, background: color }} />
      </div>
    </CardContentStack>
  )
}
