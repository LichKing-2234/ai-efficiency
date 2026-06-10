import { cn } from '@/lib/utils'

export function ProgressFraction({
  className,
  ready,
  total
}: {
  className?: string
  ready: number
  total: number
}) {
  return (
    <span className={cn('font-bold text-base leading-none tnum', className)} data-slot='progress-fraction'>
      {ready}
      <span className='text-[11px] text-[var(--ink-3)]' data-slot='progress-fraction-total'>/{total}</span>
    </span>
  )
}
