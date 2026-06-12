import type * as React from 'react'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { cn } from '@/lib/utils'

export function CardFilterBar({
  children,
  className,
  stacked = false
}: {
  children: React.ReactNode
  className?: string
  stacked?: boolean
}) {
  return (
    <CardContentStack
      dataSlot='card-filter-bar'
      className={cn(
        'border-border border-b px-[14px] py-[12px]',
        stacked ? 'flex flex-col gap-3' : 'flex flex-wrap items-center gap-2',
        className
      )}
    >
      {children}
    </CardContentStack>
  )
}
