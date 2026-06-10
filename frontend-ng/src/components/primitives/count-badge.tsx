import type * as React from 'react'
import { Badge } from '@/components/ui/badge'
import type { badgeVariants } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import type { VariantProps } from 'class-variance-authority'

export function CountBadge({
  children,
  className,
  variant
}: {
  children: React.ReactNode
  className?: string
  variant?: VariantProps<typeof badgeVariants>['variant']
}) {
  return (
    <span data-slot='count-badge'>
      <Badge className={cn('shrink-0 tnum', className)} variant={variant}>
        {children}
      </Badge>
    </span>
  )
}
