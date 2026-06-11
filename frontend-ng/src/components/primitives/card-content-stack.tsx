import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const cardContentStackGapClasses = {
  none: '',
  compact: 'gap-2',
  standard: 'gap-3',
  normal: 'gap-3.5'
}

export function CardContentStack({
  children,
  className,
  dataSlot = 'card-content',
  gap = 'standard',
  ...props
}: {
  children: React.ReactNode
  className?: string
  dataSlot?: string
  gap?: keyof typeof cardContentStackGapClasses
} & React.ComponentProps<typeof CardContent>) {
  return (
    <CardContent
      className={cn('flex flex-col', cardContentStackGapClasses[gap], className)}
      data-slot={dataSlot}
      {...props}
    >
      {children}
    </CardContent>
  )
}
