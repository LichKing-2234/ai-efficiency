import type * as React from 'react'
import { FramedCard } from '@/components/primitives/framed-card'

export function FramedTableCard({
  children,
  className,
  footer,
  header,
  ...props
}: React.ComponentProps<typeof FramedCard> & {
  footer?: React.ReactNode
  header?: React.ReactNode
}) {
  return (
    <FramedCard className={className} data-slot='framed-table-card' {...props}>
      {header}
      {children}
      {footer}
    </FramedCard>
  )
}
