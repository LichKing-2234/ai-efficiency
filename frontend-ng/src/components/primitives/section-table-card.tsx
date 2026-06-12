import type * as React from 'react'
import { Card } from '@/components/ui/card'
import { CardTableContent } from '@/components/primitives/card-table-content'
import { SectionCardHeader } from '@/components/primitives/section-card-header'

export function SectionTableCard({
  actions,
  children,
  className,
  contentClassName,
  description,
  headerClassName,
  leading,
  live,
  meta,
  title,
  ...props
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  className?: string
  contentClassName?: string
  description?: React.ReactNode
  headerClassName?: string
  leading?: React.ComponentType<{ className?: string }>
  live?: boolean
  meta?: React.ReactNode
  title: React.ReactNode
} & React.ComponentProps<typeof Card>) {
  return (
    <Card className={className} data-slot='section-table-card' {...props}>
      <SectionCardHeader
        actions={actions}
        className={headerClassName}
        description={description}
        leading={leading}
        live={live}
        meta={meta}
        title={title}
      />
      <CardTableContent className={contentClassName} variant='flush'>
        {children}
      </CardTableContent>
    </Card>
  )
}
