import type * as React from 'react'
import { Card } from '@/components/ui/card'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'

export function EntitySectionCard({
  actions,
  children,
  className,
  contentClassName,
  description,
  gap = 'standard',
  headerClassName,
  leading,
  title,
  ...props
}: {
  actions?: React.ReactNode
  children: React.ReactNode
  className?: string
  contentClassName?: string
  description?: React.ReactNode
  gap?: React.ComponentProps<typeof CardContentStack>['gap']
  headerClassName?: string
  leading?: React.ReactNode
  title: React.ReactNode
} & React.ComponentProps<typeof Card>) {
  return (
    <Card className={className} data-slot='entity-section-card' {...props}>
      <EntityCardHeader
        actions={actions}
        className={headerClassName}
        description={description}
        leading={leading}
        title={title}
      />
      <CardContentStack className={contentClassName} gap={gap}>
        {children}
      </CardContentStack>
    </Card>
  )
}
