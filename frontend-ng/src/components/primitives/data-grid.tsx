import { Slot } from 'radix-ui'
import type * as React from 'react'
import { IdentityAvatar } from './identity-avatar'
import { RecordMeta } from './record-meta'
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

export function DataGridHeaderCell({
  align = 'left',
  children,
  className
}: {
  align?: 'left' | 'right'
  children?: React.ReactNode
  className?: string
}) {
  return (
    <span className={cn(align === 'right' && 'text-right', className)} data-slot='data-grid-header-cell'>
      {children}
    </span>
  )
}

type DataGridRowBaseProps = {
  children: React.ReactNode
  className?: string
  columns: string
  fullWidth?: boolean
}

type DataGridRowDivProps = DataGridRowBaseProps & {
  as?: 'div'
} & React.HTMLAttributes<HTMLDivElement>

type DataGridRowButtonProps = DataGridRowBaseProps & {
  as: 'button'
} & React.ButtonHTMLAttributes<HTMLButtonElement>

export function DataGridRow(props: DataGridRowDivProps | DataGridRowButtonProps) {
  const { as = 'div', children, className, columns, fullWidth = false, ...rest } = props
  const sharedProps = {
    className: cn('ae-trow', as === 'button' && 'ae-trow-btn', className),
    'data-slot': 'data-grid-row',
    style: { gridTemplateColumns: normalizeColumns(columns), ...(fullWidth ? { gridColumn: '1 / -1' } : {}) }
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

export function DataGridStatusRow({
  children,
  className,
  columns,
  tone = 'empty'
}: {
  children: React.ReactNode
  className?: string
  columns: string
  tone?: 'empty' | 'loading'
}) {
  return (
    <div
      className={cn(
        'ae-trow',
        'justify-center text-center text-muted-foreground text-sm',
        tone === 'empty' && 'py-10',
        tone === 'loading' && 'py-4',
        className
      )}
      data-slot='data-grid-status-row'
      style={{ gridTemplateColumns: normalizeColumns(columns), gridColumn: '1 / -1' }}
    >
      <span>{children}</span>
    </div>
  )
}

export function DataGridCell({
  align = 'left',
  children,
  className,
  description,
  emphasis = false,
  mono = false,
  muted = false,
  numeric = false,
  tone = 'default',
  truncate = false
}: {
  align?: 'left' | 'right'
  children: React.ReactNode
  className?: string
  description?: React.ReactNode
  emphasis?: boolean
  mono?: boolean
  muted?: boolean
  numeric?: boolean
  tone?: 'default' | 'metadata' | 'muted' | 'subtle'
  truncate?: boolean
}) {
  if (description) {
    return (
      <span
        className={cn('min-w-0', align === 'right' && 'text-right', className)}
        data-slot='data-grid-cell'
      >
        <span className={cn('block font-semibold text-sm', truncate && 'truncate')} data-slot='data-grid-cell-primary'>
          {children}
        </span>
        <span className={cn('block text-muted-foreground text-xs', truncate && 'truncate')} data-slot='data-grid-cell-description'>
          {description}
        </span>
      </span>
    )
  }

  return (
    <span
      className={cn(
        align === 'right' && 'text-right',
        emphasis && 'font-semibold text-foreground',
        numeric && 'tnum',
        mono && 'mono',
        truncate && 'truncate',
        muted && 'text-muted-foreground text-xs',
        tone === 'metadata' && 'text-muted-foreground text-xs',
        tone === 'muted' && 'text-[var(--ink-2)]',
        tone === 'subtle' && 'text-[var(--ink-3)] text-xs',
        className
      )}
      data-slot='data-grid-cell'
    >
      {children}
    </span>
  )
}

export function DataGridIdentityCell({
  children,
  className,
  description,
  value
}: {
  children: React.ReactNode
  className?: string
  description?: React.ReactNode
  value: string
}) {
  return (
    <span className={cn('flex min-w-0 items-center gap-3', className)} data-slot='data-grid-identity-cell'>
      <IdentityAvatar value={value} />
      <DataGridCell description={description} truncate>{children}</DataGridCell>
    </span>
  )
}

export function DataGridRecordCell({
  children,
  className,
  description
}: {
  children: React.ReactNode
  className?: string
  description: React.ReactNode
}) {
  return (
    <span className={cn('min-w-0', className)} data-slot='data-grid-record-cell'>
      {children}
      <RecordMeta>{description}</RecordMeta>
    </span>
  )
}

export function DataGridRowAffordance({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <span className={cn('flex justify-end text-[var(--ink-4)] [&_svg]:size-4', className)} data-slot='data-grid-row-affordance'>
      {children}
    </span>
  )
}

export function DataGridPrimaryLink({
  asChild = false,
  children,
  className,
  ...props
}: React.ComponentProps<'a'> & {
  asChild?: boolean
}) {
  const Comp = asChild ? Slot.Root : 'a'
  return (
    <Comp
      className={cn('block truncate font-semibold text-foreground text-sm transition hover:text-[var(--ai-deep)]', className)}
      data-slot='data-grid-primary-link'
      {...props}
    >
      {children}
    </Comp>
  )
}
