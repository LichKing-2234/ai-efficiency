import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'

export function SplitActions({ children }: { children: React.ReactNode }) {
  return <ActionGroup dataSlot='split-actions' layout='split'>{children}</ActionGroup>
}
