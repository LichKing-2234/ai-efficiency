import type { LucideIcon } from 'lucide-react'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'

export function SectionEmptyState({
  title,
  description,
  action,
  icon: Icon
}: {
  title: string
  description?: string
  action?: React.ReactNode
  icon?: LucideIcon
}) {
  return (
    <Empty size='compact'>
      <EmptyHeader>
        {Icon ? (
          <EmptyMedia variant='icon'>
            <Icon />
          </EmptyMedia>
        ) : null}
        <EmptyTitle>{title}</EmptyTitle>
        {description ? <EmptyDescription>{description}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  )
}
