import type * as React from 'react'
import { InfoTileGrid } from '@/components/primitives/info-tile'
import { SlideOverStack } from '@/components/primitives/slide-over-stack'
import { StatusCluster } from '@/components/primitives/status-cluster'

export function DetailSummaryStack({
  children,
  metrics,
  statuses
}: {
  children?: React.ReactNode
  metrics: React.ReactNode
  statuses?: React.ReactNode
}) {
  return (
    <SlideOverStack>
      {statuses ? <StatusCluster>{statuses}</StatusCluster> : null}
      <InfoTileGrid columns={3}>
        {metrics}
      </InfoTileGrid>
      {children}
    </SlideOverStack>
  )
}
