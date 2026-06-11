import type * as React from 'react'
import { cn } from '@/lib/utils'

export function FilterRow({
  align = 'center',
  children,
  className,
  dataSlot = 'filter-row',
  gap = 'default',
  justify = 'start',
  tone = 'default',
  ...props
}: {
  align?: 'center' | 'start'
  children: React.ReactNode
  className?: string
  dataSlot?: string
  gap?: 'default' | 'lg'
  justify?: 'start' | 'between'
  tone?: 'default' | 'label'
} & React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-slot={dataSlot}
      className={cn(
        'flex flex-wrap',
        gap === 'default' ? 'gap-2' : 'gap-3',
        align === 'center' ? 'items-center' : 'items-start',
        justify === 'between' ? 'justify-between' : undefined,
        tone === 'label' ? 'text-[12px] text-[var(--ink-3)]' : undefined,
        className
      )}
      {...props}
    >
      {children}
    </div>
  )
}
