import { InfoTile } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { cn } from '@/lib/utils'

export type UsageSummaryMetric = {
  label: React.ReactNode
  value: React.ReactNode
  accent?: 'ai'
  mono?: boolean
  numeric?: boolean
}

const usageSummaryPanelFooterClass = 'mt-4 flex flex-wrap items-center justify-between gap-3 text-sm'
const usageSummaryPanelMetaClass = 'flex min-w-0 flex-wrap items-center gap-2'
const usageSummaryPanelActionsClass = 'flex flex-wrap gap-2'

function UsageSummaryPanelFooter({ children }: { children: React.ReactNode }) {
  return (
    <div
      className={usageSummaryPanelFooterClass}
      data-slot='usage-summary-panel-footer'
    >
      {children}
    </div>
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
    <div className={usageSummaryPanelMetaClass} data-slot='usage-summary-panel-meta'>
      {children}
      {summary ? <span className='text-muted-foreground'>{summary}</span> : null}
    </div>
  )
}

function UsageSummaryPanelActions({ children }: { children: React.ReactNode }) {
  return (
    <div className={usageSummaryPanelActionsClass} data-slot='usage-summary-panel-actions'>
      {children}
    </div>
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
    <InsetPanel className={cn('p-3', className)} dataSlot='usage-summary-panel'>
      <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-6'>
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
      </div>
      {(status || summary || actions) ? (
        <UsageSummaryPanelFooter>
          <UsageSummaryPanelMeta summary={summary}>{status}</UsageSummaryPanelMeta>
          {actions ? <UsageSummaryPanelActions>{actions}</UsageSummaryPanelActions> : null}
        </UsageSummaryPanelFooter>
      ) : null}
    </InsetPanel>
  )
}
