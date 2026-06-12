import type * as React from 'react'
import { FramedCard } from '@/components/primitives/framed-card'

export function WorkbenchFrame({
  body,
  footer,
  topBar
}: {
  body: React.ReactNode
  footer?: React.ReactNode
  topBar?: React.ReactNode
}) {
  return (
    <FramedCard data-slot='workbench-frame'>
      {topBar}
      {body}
      {footer}
    </FramedCard>
  )
}
