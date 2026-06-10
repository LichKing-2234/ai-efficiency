import type * as React from 'react'
import { cn } from '@/lib/utils'

export function SlideOverStack({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div data-slot='slide-over-stack' className={cn('flex flex-col gap-[18px]', className)}>
      {children}
    </div>
  )
}
