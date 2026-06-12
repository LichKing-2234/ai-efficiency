import { cn } from '@/lib/utils'

export function TopbarActionsDivider({
  className
}: {
  className?: string
}) {
  return (
    <div
      aria-hidden='true'
      className={cn('h-[22px] w-px bg-[var(--line)]', className)}
      data-slot='topbar-actions-divider'
    />
  )
}
