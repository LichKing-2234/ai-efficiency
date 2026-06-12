import type * as React from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { cn } from '@/lib/utils'

export function ToolbarActions({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return <ActionGroup align='responsive-end' className={cn('min-h-9', className)} dataSlot='toolbar-actions' wrap>{children}</ActionGroup>
}
