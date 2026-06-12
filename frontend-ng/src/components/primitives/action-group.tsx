import type * as React from 'react'
import { cn } from '@/lib/utils'

export function ActionGroup({
  align = 'end',
  children,
  className,
  dataSlot = 'action-group',
  fit = false,
  layout = 'inline',
  push = false,
  wrap = false,
  ...props
}: {
  align?: 'block-end' | 'end' | 'responsive-end' | 'start'
  children: React.ReactNode
  className?: string
  dataSlot?: string
  fit?: boolean
  layout?: 'inline' | 'split'
  push?: boolean
  wrap?: boolean
} & React.HTMLAttributes<HTMLSpanElement>) {
  return (
    <span
      data-slot={dataSlot}
      className={cn(
        'flex items-center gap-2.5',
        align === 'block-end' && 'items-end justify-end',
        align === 'start' && 'justify-start',
        align === 'end' && 'justify-end',
        align === 'responsive-end' && 'justify-start min-[920px]:justify-end',
        fit && 'min-w-0',
        layout === 'split' && 'w-full [&>*]:flex-1',
        push && 'ml-auto',
        wrap && 'flex-wrap',
        className
      )}
      {...props}
    >
      {children}
    </span>
  )
}
