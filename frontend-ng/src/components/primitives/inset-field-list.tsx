import type * as React from 'react'
import { FieldList } from '@/components/primitives/field-list'
import { InsetPanel } from '@/components/primitives/inset-panel'

export function InsetFieldList({ children }: { children: React.ReactNode }) {
  return (
    <InsetPanel stack>
      <FieldList>{children}</FieldList>
    </InsetPanel>
  )
}
