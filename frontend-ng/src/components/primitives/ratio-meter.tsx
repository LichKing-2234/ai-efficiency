import { cn } from '@/lib/utils'

export function RatioMeter({
  className,
  emptyLabel = '—',
  part,
  total
}: {
  className?: string
  emptyLabel?: React.ReactNode
  part: number
  total: number
}) {
  const width = part <= 0 || total <= 0 ? 0 : Math.max(4, Math.min(100, (part / total) * 100))
  const empty = total <= 0

  return (
    <span className={cn('flex min-w-0 items-center gap-2', className)} data-empty={empty ? 'true' : undefined} data-slot='ratio-meter'>
      <span className='h-1.5 max-w-20 flex-1 overflow-hidden rounded-full bg-[var(--surface-inset)]' data-slot='ratio-meter-track'>
        <span
          className='block h-full rounded-full bg-[var(--ai)]'
          data-slot='ratio-meter-fill'
          style={{ width: `${width}%` }}
        />
      </span>
      <span className='mono tnum min-w-12 text-[var(--ink-2)] text-xs' data-slot='ratio-meter-value'>
        {empty ? emptyLabel : `${part}/${total}`}
      </span>
    </span>
  )
}
