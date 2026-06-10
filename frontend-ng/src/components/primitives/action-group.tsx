import type * as React from 'react'
import { cn } from '@/lib/utils'

export function ActionGroup({
  align = 'end',
  children,
  className,
  layout = 'inline',
  wrap = false
}: {
  align?: 'end' | 'start'
  children: React.ReactNode
  className?: string
  layout?: 'inline' | 'split'
  wrap?: boolean
}) {
  return (
    <span
      data-slot='action-group'
      className={cn(
        'flex items-center gap-2',
        align === 'start' ? 'justify-start' : 'justify-end',
        layout === 'split' && 'w-full [&>*]:flex-1',
        wrap && 'flex-wrap',
        className
      )}
    >
      {children}
    </span>
  )
}
