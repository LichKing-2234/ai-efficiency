import { ActionGroup } from '@/components/primitives/action-group'
import { FilterRow } from '@/components/primitives/filter-row'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { cn } from '@/lib/utils'

export type UsageSummaryMetric = {
  label: React.ReactNode
  value: React.ReactNode
  accent?: 'ai'
  mono?: boolean
  numeric?: boolean
}

function UsageSummaryPanelFooter({ children }: { children: React.ReactNode }) {
  return (
    <FilterRow className='mt-[12px] border-t border-[var(--line)] pt-[12px] text-[12px]' dataSlot='usage-summary-panel-footer' justify='between' gap='lg'>
      {children}
    </FilterRow>
  )
}

function UsageSummaryPanelMeta({
  children,
  summary
}: {
  children?: React.ReactNode
  summary?: React.ReactNode
}) {
  return (
    <FilterRow className='min-w-0' dataSlot='usage-summary-panel-meta'>
      {children}
      {summary ? <span className='text-[11.5px] text-[var(--ink-3)]'>{summary}</span> : null}
    </FilterRow>
  )
}

function UsageSummaryPanelActions({ children }: { children: React.ReactNode }) {
  return (
    <ActionGroup dataSlot='usage-summary-panel-actions' wrap>
      {children}
    </ActionGroup>
  )
}

export function UsageSummaryPanel({
  actions,
  className,
  metrics,
  status,
  summary
}: {
  actions?: React.ReactNode
  className?: string
  metrics: Array<UsageSummaryMetric>
  status?: React.ReactNode
  summary?: React.ReactNode
}) {
  return (
    <InsetPanel className={cn('bg-[var(--surface)] p-[14px]', className)} dataSlot='usage-summary-panel'>
      <InfoTileGrid className='min-[560px]:grid-cols-2 min-[1100px]:grid-cols-6' columns={3}>
        {metrics.map((metric, index) => (
          <InfoTile
            accent={metric.accent}
            compact
            key={`${String(metric.label)}-${index}`}
            label={metric.label}
            mono={metric.mono}
            numeric={metric.numeric}
            value={metric.value}
          />
        ))}
      </InfoTileGrid>
      {(status || summary || actions) ? (
        <UsageSummaryPanelFooter>
          <UsageSummaryPanelMeta summary={summary}>{status}</UsageSummaryPanelMeta>
          {actions ? <UsageSummaryPanelActions>{actions}</UsageSummaryPanelActions> : null}
        </UsageSummaryPanelFooter>
      ) : null}
    </InsetPanel>
  )
}
