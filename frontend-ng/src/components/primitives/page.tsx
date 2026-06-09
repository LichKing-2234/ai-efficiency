import { cn } from '@/lib/utils'

export function Page({ children, className }: { children: React.ReactNode; className?: string }) {
  return <div className={cn('page-fade flex flex-col gap-5', className)}>{children}</div>
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
    return (
      <div className='flex flex-col gap-3 md:flex-row md:items-center md:justify-between'>
        {description ? <p className='max-w-3xl text-muted-foreground text-sm'>{description}</p> : <span className='hidden' aria-hidden='true'>{title}</span>}
        {actions ? <div className='flex shrink-0 items-center gap-2'>{actions}</div> : null}
      </div>
    )
  }

  return (
    <div className='flex flex-col gap-3 md:flex-row md:items-end md:justify-between'>
      <div className='min-w-0'>
        <h1 className='font-semibold text-2xl tracking-tight'>{title}</h1>
        {description ? <p className='mt-1 max-w-3xl text-muted-foreground text-sm'>{description}</p> : null}
      </div>
      {actions ? <div className='flex shrink-0 items-center gap-2'>{actions}</div> : null}
    </div>
  )
}
