import { cn } from '@/lib/utils'

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
    <div className={cn('flex flex-wrap gap-4', compact && 'gap-3', className)} data-slot='chart-legend'>
      {items.map((item) => (
        <span className='flex items-center gap-1.5 text-[12px] text-[var(--ink-2)]' data-slot='chart-legend-item' key={String(item.label)}>
          <span className='size-2.5 rounded-[3px]' data-slot='chart-legend-swatch' style={{ background: item.color }} />
          <span className='min-w-0 truncate'>{item.label}</span>
        </span>
      ))}
    </div>
  )
}
