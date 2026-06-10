import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function CardContentStack({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <CardContent className={cn('flex flex-col gap-3', className)}>
      {children}
    </CardContent>
  )
}
