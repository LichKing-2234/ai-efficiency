import { cn } from '@/lib/utils'

export function TokenMeter({
  className,
  label,
  max,
  value
}: {
  className?: string
  label: React.ReactNode
  max: number
  value: number
}) {
  const width = value <= 0 || max <= 0 ? 0 : Math.max(4, Math.min(100, (value / max) * 100))

  return (
    <span className={cn('flex min-w-0 items-center gap-2', className)} data-slot='token-meter'>
      <span className='h-1.5 max-w-20 flex-1 overflow-hidden rounded-full bg-[var(--surface-inset)]' data-slot='token-meter-track'>
        <span
          className='block h-full rounded-full bg-[var(--ai)]'
          data-slot='token-meter-fill'
          style={{ width: `${width}%` }}
        />
      </span>
      <span className='mono tnum min-w-12 text-[var(--ink-2)] text-xs' data-slot='token-meter-value'>
        {label}
      </span>
    </span>
  )
}
