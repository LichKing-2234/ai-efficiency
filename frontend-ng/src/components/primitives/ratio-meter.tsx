import { ActionGroup } from '@/components/primitives/action-group'
import { MeterTrack } from '@/components/primitives/meter-track'
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
    <ActionGroup className={className} data-empty={empty ? 'true' : undefined} dataSlot='ratio-meter' fit>
      <MeterTrack className='h-1.5 max-w-[88px] flex-1' dataSlot='ratio-meter-track'>
        <span
          className='block h-full rounded-full bg-[var(--ai)]'
          data-slot='ratio-meter-fill'
          style={{ width: `${width}%` }}
        />
      </MeterTrack>
      <span className='mono tnum min-w-[54px] text-[11.5px] text-[var(--ink-2)]' data-slot='ratio-meter-value'>
        {empty ? emptyLabel : `${part}/${total}`}
      </span>
    </ActionGroup>
  )
}
