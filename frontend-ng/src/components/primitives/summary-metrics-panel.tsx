import type * as React from 'react'
import { InfoTileGrid } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { SectionCard } from '@/components/primitives/section-card'

export function SummaryMetricsPanel({
  children,
  note,
  title
}: {
  children: React.ReactNode
  note?: React.ReactNode
  title: string
}) {
  return (
    <SectionCard title={title}>
      <InfoTileGrid columns={4}>
        {children}
      </InfoTileGrid>
      {note ? <InsetPanel muted>{note}</InsetPanel> : null}
    </SectionCard>
  )
}
