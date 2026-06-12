import type * as React from 'react'
import { InsetPanel } from '@/components/primitives/inset-panel'

export function ProviderTestResponse({ children }: { children: React.ReactNode }) {
  return (
    <InsetPanel comfortable dataSlot='provider-test-response'>
      {children}
    </InsetPanel>
  )
}
