import type * as React from 'react'
import { cn } from '@/lib/utils'

const controlGridVariants = {
  subscription: 'min-[920px]:grid-cols-[150px_150px_minmax(0,1fr)_minmax(0,1fr)_120px_auto]',
  'inline-actions': 'min-[1100px]:grid-cols-[minmax(0,1fr)_auto_auto]',
  'two-column': 'min-[920px]:grid-cols-2'
}

export function ControlGrid({
  children,
  className,
  variant = 'inline-actions'
}: {
  children: React.ReactNode
  className?: string
  variant?: keyof typeof controlGridVariants
}) {
  return (
    <div data-slot='control-grid' className={cn('grid gap-3', controlGridVariants[variant], className)}>
      {children}
    </div>
  )
}
