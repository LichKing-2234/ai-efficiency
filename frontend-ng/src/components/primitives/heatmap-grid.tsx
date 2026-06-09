import { cn } from '@/lib/utils'

export type HeatmapPoint = {
  day: number
  hour: number
  value: number
}

export type HeatmapCell = HeatmapPoint & {
  intensity: number
}

export function buildHeatmapCells(points: HeatmapPoint[]) {
  const buckets = new Map<string, number>()
  for (const point of points) {
    if (point.day < 0 || point.day > 6 || point.hour < 0 || point.hour > 23) continue
    const key = `${point.day}:${point.hour}`
    buckets.set(key, (buckets.get(key) ?? 0) + Math.max(0, point.value))
  }
  const max = Math.max(1, ...buckets.values())
  const cells: HeatmapCell[] = []
  for (let day = 0; day < 7; day += 1) {
    for (let hour = 0; hour < 24; hour += 1) {
      const value = buckets.get(`${day}:${hour}`) ?? 0
      cells.push({ day, hour, value, intensity: value / max })
    }
  }
  return cells
}

export function HeatmapGrid({
  points,
  dayLabels,
  lessLabel,
  moreLabel,
  valueFormatter = (value) => String(value),
  className
}: {
  points: HeatmapPoint[]
  dayLabels: string[]
  lessLabel: string
  moreLabel: string
  valueFormatter?: (value: number) => string
  className?: string
}) {
  const cells = buildHeatmapCells(points)
  const labels = dayLabels.length >= 7 ? dayLabels : ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

  return (
    <div className={cn('flex flex-col gap-3', className)}>
      <div className='flex items-center justify-end gap-2 text-[11.5px] text-muted-foreground'>
        <span>{lessLabel}</span>
        <span className='flex gap-1'>
          {[0.08, 0.28, 0.48, 0.7, 1].map((intensity) => (
            <span
              aria-hidden='true'
              className='size-3 rounded-[3px]'
              key={intensity}
              style={{ background: heatmapColor(intensity) }}
            />
          ))}
        </span>
        <span>{moreLabel}</span>
      </div>
      <div className='grid min-w-0 grid-cols-[34px_minmax(0,1fr)] gap-2'>
        <div />
        <div className='grid gap-1' style={{ gridTemplateColumns: 'repeat(24, minmax(0, 1fr))' }}>
          {Array.from({ length: 24 }, (_, hour) => (
            <div className='mono text-center text-[9px] text-[var(--ink-4)]' key={hour}>
              {hour % 6 === 0 ? hour : ''}
            </div>
          ))}
        </div>
        {labels.slice(0, 7).map((label, day) => (
          <div className='contents' key={label}>
            <div className='flex items-center font-medium text-[11px] text-muted-foreground'>{label}</div>
            <div className='grid gap-1' style={{ gridTemplateColumns: 'repeat(24, minmax(0, 1fr))' }}>
              {cells.slice(day * 24, day * 24 + 24).map((cell) => (
                <div
                  aria-label={`${label} ${cell.hour}:00 ${valueFormatter(cell.value)}`}
                  className='aspect-square rounded-[3px] transition-transform hover:scale-110'
                  key={`${cell.day}-${cell.hour}`}
                  style={{ background: heatmapColor(cell.intensity) }}
                  title={`${label} ${cell.hour}:00 · ${valueFormatter(cell.value)}`}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function heatmapColor(intensity: number) {
  const value = Math.max(0.08, Math.min(1, intensity))
  return `color-mix(in oklab, var(--ai) ${(value * 92).toFixed(0)}%, var(--surface-inset))`
}
