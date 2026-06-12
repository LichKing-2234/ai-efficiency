import type * as React from 'react'
import { InfoTileGrid } from '@/components/primitives/info-tile'
import { MutedInsetNote } from '@/components/primitives/muted-inset-note'
import { SectionCard } from '@/components/primitives/section-card'

export function SummaryMetricsPanel({
  actions,
  children,
  description,
  gap = 'standard',
  leading,
  live = false,
  metricsClassName,
  metricsColumns = 4,
  note,
  title
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  description?: React.ReactNode
  gap?: React.ComponentProps<typeof SectionCard>['gap']
  leading?: React.ComponentProps<typeof SectionCard>['leading']
  live?: boolean
  metricsClassName?: string
  metricsColumns?: 2 | 3 | 4
  note?: React.ReactNode
  title: string
}) {
  return (
    <SectionCard actions={actions} description={description} gap={gap} leading={leading} live={live} title={title}>
      <InfoTileGrid className={metricsClassName} columns={metricsColumns}>
        {children}
      </InfoTileGrid>
      {note ? <MutedInsetNote>{note}</MutedInsetNote> : null}
    </SectionCard>
  )
}
