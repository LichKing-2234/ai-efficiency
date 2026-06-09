import type { LucideIcon } from 'lucide-react'
import { ArrowDownIcon, ArrowUpIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

function Sparkline({
  data,
  color = 'var(--ai)'
}: {
  data: number[]
  color?: string
}) {
  const width = 92
  const height = 30
  const min = Math.min(...data)
  const max = Math.max(...data)
  const range = max - min || 1
  const points = data.map((value, index) => {
    const x = data.length === 1 ? width : (index / (data.length - 1)) * width
    const y = height - 3 - ((value - min) / range) * (height - 6)
    return [x, y] as const
  })
  const line = points.map(([x, y], index) => `${index === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ')
  const area = `${line} L${width} ${height} L0 ${height} Z`
  const last = points[points.length - 1] ?? [0, height / 2]
  const gradientId = `spark-${data.join('-').replace(/[^a-zA-Z0-9-]/g, '')}`

  return (
    <svg aria-hidden='true' className='block overflow-visible' height={height} viewBox={`0 0 ${width} ${height}`} width={width}>
      <defs>
        <linearGradient id={gradientId} x1='0' x2='0' y1='0' y2='1'>
          <stop offset='0%' stopColor={color} stopOpacity='.22' />
          <stop offset='100%' stopColor={color} stopOpacity='0' />
        </linearGradient>
      </defs>
      <path d={area} fill={`url(#${gradientId})`} />
      <path d={line} fill='none' stroke={color} strokeLinecap='round' strokeLinejoin='round' strokeWidth='1.6' />
      <circle cx={last[0]} cy={last[1]} fill={color} r='2.2' />
    </svg>
  )
}

export function MetricCard({
  label,
  value,
  helper,
  accent = false,
  icon: Icon,
  delta,
  deltaTone,
  sparkline,
  sparklineColor
}: {
  label: string
  value: React.ReactNode
  helper?: React.ReactNode
  accent?: boolean
  icon?: LucideIcon
  delta?: number
  deltaTone?: 'pos' | 'neg'
  sparkline?: number[]
  sparklineColor?: string
}) {
  const tone = deltaTone ?? ((delta ?? 0) >= 0 ? 'pos' : 'neg')
  return (
    <Card
      className={cn(
        'overflow-hidden',
        accent && 'border-[var(--ai-line)] bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]'
      )}
    >
      <CardContent className='flex flex-col gap-3 p-[18px]'>
        <div className='flex items-center gap-2'>
          {Icon ? (
            <span
              className={cn(
                'grid size-[26px] shrink-0 place-items-center rounded-[var(--r-sm)] border',
                accent
                  ? 'border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]'
                  : 'border-border bg-[var(--surface-inset)] text-[var(--ink-3)]'
              )}
            >
              <Icon className='size-3.5' />
            </span>
          ) : null}
          <div className='min-w-0 flex-1 truncate text-muted-foreground text-xs'>{label}</div>
          {typeof delta === 'number' ? (
            <Badge variant={tone}>
              {delta >= 0 ? <ArrowUpIcon className='size-3' /> : <ArrowDownIcon className='size-3' />}
              {Math.abs(delta)}%
            </Badge>
          ) : null}
        </div>
        <div className='flex items-end justify-between gap-3'>
          <div className={cn('tnum font-semibold text-3xl leading-none tracking-tight', accent && 'text-[var(--ai-deep)]')}>
            {value}
          </div>
          {sparkline?.length ? <Sparkline color={sparklineColor ?? (accent ? 'var(--ai)' : 'var(--viz-output)')} data={sparkline} /> : null}
        </div>
        {helper ? <div className='text-muted-foreground text-xs'>{helper}</div> : null}
      </CardContent>
    </Card>
  )
}
