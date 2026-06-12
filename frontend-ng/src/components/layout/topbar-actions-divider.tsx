import { cn } from '@/lib/utils'

export function TopbarActionsDivider({
  className
}: {
  className?: string
}) {
  return (
    <div
      aria-hidden='true'
      className={cn('hidden h-[22px] w-px bg-[var(--line)] min-[920px]:block', className)}
      data-slot='topbar-actions-divider'
    />
  )
}
