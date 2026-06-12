import type * as React from 'react'
import { DetailSection } from '@/components/primitives/detail-section'
import { FieldList } from '@/components/primitives/field-list'

export function DetailFieldSection({
  children,
  title
}: {
  children: React.ReactNode
  title: React.ReactNode
}) {
  return (
    <DetailSection title={title}>
      <FieldList>{children}</FieldList>
    </DetailSection>
  )
}
