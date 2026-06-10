import type * as React from 'react'
import { CardContent } from '@/components/ui/card'
import { cn } from '@/lib/utils'

const cardTableContentVariants = {
  edge: 'px-0 pb-0',
  flush: 'p-0'
}

export function CardTableContent({
  children,
  className,
  variant = 'edge'
}: {
  children: React.ReactNode
  className?: string
  variant?: keyof typeof cardTableContentVariants
}) {
  return (
    <CardContent className={cn(cardTableContentVariants[variant], className)} data-layout='table' data-variant={variant}>
      {children}
    </CardContent>
  )
}
