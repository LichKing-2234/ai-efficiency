import type * as React from 'react'
import { DetailSection } from '@/components/primitives/detail-section'
import { LinkedRecordList } from '@/components/primitives/linked-record-list'
import { PageEmpty } from '@/components/primitives/page-empty'

export function DetailRecordLinksSection({
  children,
  emptyTitle,
  title
}: {
  children?: React.ReactNode
  emptyTitle: string
  title: React.ReactNode
}) {
  return (
    <DetailSection title={title}>
      {children ? <LinkedRecordList>{children}</LinkedRecordList> : <PageEmpty title={emptyTitle} />}
    </DetailSection>
  )
}
