import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function CardTableContent({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <CardContent className={cn('px-0 pb-0', className)} data-layout='table'>
      {children}
    </CardContent>
  )
}
