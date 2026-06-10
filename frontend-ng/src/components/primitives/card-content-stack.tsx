import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const cardContentStackGapClasses = {
  compact: 'gap-2',
  standard: 'gap-3',
  normal: 'gap-4'
}

export function CardContentStack({
  children,
  className,
  gap = 'standard'
}: {
  children: React.ReactNode
  className?: string
  gap?: keyof typeof cardContentStackGapClasses
}) {
  return (
    <CardContent className={cn('flex flex-col', cardContentStackGapClasses[gap], className)}>
      {children}
    </CardContent>
  )
}
