import type * as React from 'react'
import { Card } from '@/components/ui/card'

export function AccentSurfaceCard({
  children,
  dataSlot = 'accent-surface-card',
  ...props
}: React.ComponentProps<typeof Card> & {
  dataSlot?: string
}) {
  return (
    <Card data-slot={dataSlot} variant='accent' {...props}>
      {children}
    </Card>
  )
}
