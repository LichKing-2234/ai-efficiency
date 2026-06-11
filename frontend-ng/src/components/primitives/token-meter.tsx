import { ActionGroup } from '@/components/primitives/action-group'
import { MeterTrack } from '@/components/primitives/meter-track'
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
    <ActionGroup className={cn('gap-2', className)} dataSlot='token-meter' fit>
      <MeterTrack className='h-1.5 max-w-[88px] flex-1' dataSlot='token-meter-track'>
        <span
          className='block h-full rounded-full bg-[var(--ai)]'
          data-slot='token-meter-fill'
          style={{ width: `${width}%` }}
        />
      </MeterTrack>
      <span className='mono tnum min-w-[54px] text-right text-[11.5px] text-[var(--ink-2)]' data-slot='token-meter-value'>
        {label}
      </span>
    </ActionGroup>
  )
}
