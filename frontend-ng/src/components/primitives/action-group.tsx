import type * as React from 'react'
import { cn } from '@/lib/utils'

export function ActionGroup({
  align = 'end',
  children,
  className,
  fit = false,
  layout = 'inline',
  wrap = false
}: {
  align?: 'end' | 'start'
  children: React.ReactNode
  className?: string
  fit?: boolean
  layout?: 'inline' | 'split'
  wrap?: boolean
}) {
  return (
    <span
      data-slot='action-group'
      className={cn(
        'flex items-center gap-2',
        align === 'start' ? 'justify-start' : 'justify-end',
        fit && 'min-w-0',
        layout === 'split' && 'w-full [&>*]:flex-1',
        wrap && 'flex-wrap',
        className
      )}
    >
      {children}
    </span>
  )
}
