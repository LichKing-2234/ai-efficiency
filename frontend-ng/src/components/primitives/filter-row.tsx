import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FilterRow({
  align = 'center',
  children,
  className,
  gap = 'default',
  justify = 'start',
  tone = 'default'
}: {
  align?: 'center' | 'start'
  children: React.ReactNode
  className?: string
  gap?: 'default' | 'lg'
  justify?: 'start' | 'between'
  tone?: 'default' | 'label'
}) {
  return (
    <div
      data-slot='filter-row'
      className={cn(
        'flex flex-wrap',
        gap === 'default' ? 'gap-2' : 'gap-3',
        align === 'center' ? 'items-center' : 'items-start',
        justify === 'between' ? 'justify-between' : undefined,
        tone === 'label' ? 'text-sm' : undefined,
        className
      )}
    >
      {children}
    </div>
  )
}
