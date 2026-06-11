import type * as React from 'react'
import { cn } from '@/lib/utils'

const stackGapClasses = {
  none: '',
  compact: 'gap-2',
  normal: 'gap-4',
  loose: 'gap-5'
}

export function Stack({
  children,
  className,
  constrain,
  dataSlot = 'stack',
  gap = 'normal',
  ...props
}: {
  children: React.ReactNode
  className?: string
  constrain?: 'content'
  dataSlot?: string
  gap?: keyof typeof stackGapClasses
} & React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      data-slot={dataSlot}
      className={cn('flex flex-col', stackGapClasses[gap], constrain === 'content' && 'min-w-0', className)}
      {...props}
    >
      {children}
    </div>
  )
}
