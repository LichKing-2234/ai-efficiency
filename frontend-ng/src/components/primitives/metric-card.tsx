import type { LucideIcon } from 'lucide-react'
import { ArrowDownIcon, ArrowUpIcon } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Card } from '@/components/ui/card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { Sparkline } from '@/components/primitives/charts'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'
import { ActionGroup } from './action-group'

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
      <CardContentStack className='p-[18px]'>
        <ActionGroup align='start' className='min-w-0' dataSlot='kpi-card-header' fit>
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
          <div className='min-w-0 flex-1 truncate text-[12px] font-medium text-[var(--ink-3)]'>{label}</div>
          {typeof delta === 'number' ? (
            <Badge variant={tone}>
              {delta >= 0 ? <ArrowUpIcon className='size-3' /> : <ArrowDownIcon className='size-3' />}
              {Math.abs(delta)}%
            </Badge>
          ) : null}
        </ActionGroup>
        <ActionGroup align='block-end' className='gap-3' dataSlot='kpi-card-value-row' fit layout='split'>
          <div className={cn('tnum font-semibold text-3xl leading-none tracking-tight', accent && 'text-[var(--ai-deep)]')}>
            {value}
          </div>
          {sparkline?.length ? (
            <Stack className='items-end' dataSlot='kpi-card-sparkline' gap='none'>
              <Sparkline color={sparklineColor ?? (accent ? 'var(--ai)' : 'var(--viz-output)')} data={sparkline} height={30} width={92} />
            </Stack>
          ) : null}
        </ActionGroup>
        {helper ? <Stack className='gap-3 text-[11.5px] text-[var(--ink-3)]' dataSlot='kpi-card-helper' gap='none'>{helper}</Stack> : null}
      </CardContentStack>
    </Card>
  )
}

export const MetricCard = KpiCard
