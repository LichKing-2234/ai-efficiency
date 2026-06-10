import { cn } from '@/lib/utils'

const pageHeaderDescriptionClass = 'mt-1 max-w-3xl text-muted-foreground text-sm'

function PageHeaderDescription({ children }: { children: React.ReactNode }) {
  return (
    <p className={pageHeaderDescriptionClass} data-slot='page-header-description'>
      {children}
    </p>
  )
}

export function Page({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('page-fade flex flex-col gap-5', className)}>{children}</div>
}

export function PageToolbar({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('flex items-center justify-end gap-2', className)}>{children}</div>
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
    <div className='flex flex-col gap-3 md:flex-row md:items-end md:justify-between'>
      <div className='min-w-0'>
        <h1 className='font-semibold text-2xl tracking-tight'>{title}</h1>
        {description ? <PageHeaderDescription>{description}</PageHeaderDescription> : null}
      </div>
      {actions ? <div className='flex shrink-0 items-center gap-2'>{actions}</div> : null}
    </div>
  )
}
