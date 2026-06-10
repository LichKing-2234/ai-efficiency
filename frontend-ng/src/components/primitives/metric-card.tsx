import type { LucideIcon } from 'lucide-react'
import { ArrowDownIcon, ArrowUpIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'
import { Sparkline } from '@/components/primitives/charts'
import { cn } from '@/lib/utils'

export function KpiCard({
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
          {sparkline?.length ? <Sparkline color={sparklineColor ?? (accent ? 'var(--ai)' : 'var(--viz-output)')} data={sparkline} height={30} width={92} /> : null}
        </div>
        {helper ? <div className='text-muted-foreground text-xs'>{helper}</div> : null}
      </CardContent>
    </Card>
  )
}

export const MetricCard = KpiCard
