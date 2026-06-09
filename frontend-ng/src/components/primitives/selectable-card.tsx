import type * as React from 'react'
import { cn } from '@/lib/utils'

export function SelectableCard({
  active,
  className,
  children,
  type = 'button',
  ...props
}: React.ComponentProps<'button'> & {
  active: boolean
}) {
  return (
    <button
      aria-pressed={active}
      className={cn(
        'rounded-[var(--r-md)] border border-border bg-card p-3 text-left transition hover:border-[var(--line-strong)] hover:bg-[var(--surface-2)] data-[active=true]:border-[var(--ai-line)] data-[active=true]:bg-[var(--ai-softer)]',
        className
      )}
      data-active={active}
      type={type}
      {...props}
    >
      {children}
    </button>
  )
}
