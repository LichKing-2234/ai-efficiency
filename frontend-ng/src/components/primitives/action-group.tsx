import type * as React from 'react'
import { cn } from '@/lib/utils'

export function ActionGroup({
  children,
  className,
  wrap = false
}: {
  children: React.ReactNode
  className?: string
  wrap?: boolean
}) {
  return (
    <span
      data-slot='action-group'
      className={cn('flex items-center justify-end gap-2', wrap && 'flex-wrap', className)}
    >
      {children}
    </span>
  )
}
