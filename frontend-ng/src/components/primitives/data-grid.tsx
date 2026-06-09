import { cn } from '@/lib/utils'

function normalizeColumns(columns: string) {
  return columns.replaceAll('_', ' ')
}

export function DataGrid({
  children,
  className,
  minWidth,
  scrollClassName
}: {
  children: React.ReactNode
  className?: string
  minWidth?: number
  scrollClassName?: string
}) {
  return (
    <div className={cn('overflow-x-auto', scrollClassName)} data-slot='data-grid-scroll'>
      <div className={cn('ae-table', className)} data-slot='data-grid' style={minWidth ? { minWidth } : undefined}>
        {children}
      </div>
    </div>
  )
}

export function DataGridHeader({
  children,
  className,
  columns
}: {
  children: React.ReactNode
  className?: string
  columns: string
}) {
  return (
    <div
      className={cn('ae-thead', className)}
      data-slot='data-grid-header'
      style={{ gridTemplateColumns: normalizeColumns(columns) }}
    >
      {children}
    </div>
  )
}

type DataGridRowBaseProps = {
  children: React.ReactNode
  className?: string
  columns: string
}

type DataGridRowDivProps = DataGridRowBaseProps & {
  as?: 'div'
} & React.HTMLAttributes<HTMLDivElement>

type DataGridRowButtonProps = DataGridRowBaseProps & {
  as: 'button'
} & React.ButtonHTMLAttributes<HTMLButtonElement>

export function DataGridRow(props: DataGridRowDivProps | DataGridRowButtonProps) {
  const { as = 'div', children, className, columns, ...rest } = props
  const sharedProps = {
    className: cn('ae-trow', as === 'button' && 'ae-trow-btn', className),
    'data-slot': 'data-grid-row',
    style: { gridTemplateColumns: normalizeColumns(columns) }
  }

  if (as === 'button') {
    return (
      <button {...(rest as React.ButtonHTMLAttributes<HTMLButtonElement>)} {...sharedProps} type={(rest as React.ButtonHTMLAttributes<HTMLButtonElement>).type ?? 'button'}>
        {children}
      </button>
    )
  }

  return (
    <div {...(rest as React.HTMLAttributes<HTMLDivElement>)} {...sharedProps}>
      {children}
    </div>
  )
}
