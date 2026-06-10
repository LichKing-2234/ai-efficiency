import type * as React from 'react'
import { cn } from '@/lib/utils'

const stackGapClasses = {
  compact: 'gap-2',
  normal: 'gap-4',
  loose: 'gap-5'
}

export function Stack({
  children,
  className,
  constrain,
  gap = 'normal'
}: {
  children: React.ReactNode
  className?: string
  constrain?: 'content'
  gap?: keyof typeof stackGapClasses
}) {
  return (
    <div data-slot='stack' className={cn('flex flex-col', stackGapClasses[gap], constrain === 'content' && 'min-w-0', className)}>
      {children}
    </div>
  )
}
