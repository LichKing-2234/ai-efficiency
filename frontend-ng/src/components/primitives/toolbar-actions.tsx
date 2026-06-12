import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function ToolbarActions({ children }: { children: React.ReactNode }) {
  return <ActionGroup dataSlot='toolbar-actions' wrap>{children}</ActionGroup>
}
