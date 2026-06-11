import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'
import { FilterRow } from './filter-row'

export type ChartLegendItem = {
  label: React.ReactNode
  color: string
}

export function ChartLegend({
  className,
  compact = false,
  items
}: {
  className?: string
  compact?: boolean
  items: ChartLegendItem[]
}) {
  return (
    <FilterRow className={cn(compact ? 'gap-3' : 'gap-4', className)} dataSlot='chart-legend'>
      {items.map((item) => (
        <ActionGroup align='start' className='gap-1.5 text-[12px] text-[var(--ink-2)]' dataSlot='chart-legend-item' key={String(item.label)}>
          <span className='size-2.5 rounded-[3px]' data-slot='chart-legend-swatch' style={{ background: item.color }} />
          <span className='min-w-0 truncate'>{item.label}</span>
        </ActionGroup>
      ))}
    </FilterRow>
  )
}
