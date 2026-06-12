import type * as React from 'react'
import type { VariantProps } from 'class-variance-authority'
import { Badge } from '@/components/ui/badge'
import type { badgeVariants } from '@/components/ui/badge'

export function CategoryBadge({
  children,
  variant = 'secondary'
}: {
  children: React.ReactNode
  variant?: VariantProps<typeof badgeVariants>['variant']
}) {
  return <Badge variant={variant}>{children}</Badge>
}
