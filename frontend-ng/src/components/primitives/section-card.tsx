import type * as React from 'react'
import { Card } from '@/components/ui/card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { SectionCardHeader } from '@/components/primitives/section-card-header'

export function SectionCard({
  actions,
  children,
  className,
  contentClassName,
  contentDataSlot,
  description,
  gap = 'standard',
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
  contentDataSlot?: string
  description?: React.ReactNode
  gap?: React.ComponentProps<typeof CardContentStack>['gap']
  headerClassName?: string
  leading?: React.ComponentType<{ className?: string }>
  live?: boolean
  meta?: React.ReactNode
  title: React.ReactNode
} & React.ComponentProps<typeof Card>) {
  return (
    <Card className={className} data-slot='section-card' {...props}>
      <SectionCardHeader
        actions={actions}
        className={headerClassName}
        description={description}
        leading={leading}
        live={live}
        meta={meta}
        title={title}
      />
      <CardContentStack className={contentClassName} dataSlot={contentDataSlot} gap={gap}>
        {children}
      </CardContentStack>
    </Card>
  )
}
