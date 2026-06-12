import type * as React from 'react'
import { cn } from '@/lib/utils'

const surfaceSplitVariants = {
  equal: 'split-equal',
  overview: 'split-2',
  rail: 'split-rail',
  settings: 'split-settings'
} as const

export function SurfaceSplit({
  children,
  className,
  variant
}: {
  children: React.ReactNode
  className?: string
  variant: keyof typeof surfaceSplitVariants
}) {
  return (
    <div className={cn(surfaceSplitVariants[variant], className)} data-slot='surface-split' data-variant={variant}>
      {children}
    </div>
  )
}
