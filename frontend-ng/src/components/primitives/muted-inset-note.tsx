import type * as React from 'react'
import { InsetPanel } from '@/components/primitives/inset-panel'

export function MutedInsetNote({
  children,
  compact = false
}: {
  children: React.ReactNode
  compact?: boolean
}) {
  return (
    <InsetPanel compact={compact} dataSlot='muted-inset-note' muted>
      {children}
    </InsetPanel>
  )
}
