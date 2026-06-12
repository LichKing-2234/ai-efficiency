import { ActionGroup } from '@/components/primitives/action-group'
import { Stack } from '@/components/primitives/stack'
import { cn } from '@/lib/utils'

const pageHeaderDescriptionClass = 'mt-1 max-w-3xl text-[12px] text-[var(--ink-3)]'

function PageHeaderDescription({ children }: { children: React.ReactNode }) {
  return (
    <p className={pageHeaderDescriptionClass} data-slot='page-header-description'>
      {children}
    </p>
  )
}

export function Page({ children, className }: { children: React.ReactNode; className?: string }) {
  return <Stack className={cn('page-fade', className)} gap='loose'>{children}</Stack>
}

export function PageToolbar({ children, className }: { children: React.ReactNode; className?: string }) {
  return <ActionGroup className={className}>{children}</ActionGroup>
}

export function PageHeader({
  title,
  description,
  actions,
  variant = 'title'
}: {
  title: string
  description?: React.ReactNode
  actions?: React.ReactNode
  variant?: 'title' | 'toolbar'
}) {
  if (variant === 'toolbar') {
    if (!actions) return null
    return <PageToolbar>{actions}</PageToolbar>
  }

  return (
    <ActionGroup align='responsive-end' className='min-[920px]:items-end' dataSlot='page-header' fit layout='split'>
      <Stack className='min-w-0' dataSlot='page-header-copy' gap='none'>
        <h1 className='font-semibold text-2xl tracking-tight'>{title}</h1>
        {description ? <PageHeaderDescription>{description}</PageHeaderDescription> : null}
      </Stack>
      {actions ? <ActionGroup className='shrink-0' dataSlot='page-header-actions'>{actions}</ActionGroup> : null}
    </ActionGroup>
  )
}
