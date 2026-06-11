import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'
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
    <CardContentStack className={cn(cardTableContentVariants[variant], className)} data-layout='table' data-variant={variant} gap='none'>
      {children}
    </CardContentStack>
  )
}
