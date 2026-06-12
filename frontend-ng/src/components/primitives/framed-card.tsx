import type * as React from 'react'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'

export function FramedCard({
  children,
  className,
  ...props
}: React.ComponentProps<typeof Card>) {
  return (
    <Card
      className={cn('overflow-hidden', className)}
      data-slot='framed-card'
      {...props}
    >
      {children}
    </Card>
  )
}
