import { Skeleton } from '@/components/ui/skeleton'

export function LoadingChip({
  ariaLabel,
  className = 'h-5 w-40'
}: {
  ariaLabel: string
  className?: string
}) {
  return (
    <span data-slot='loading-chip'>
      <Skeleton
        aria-label={ariaLabel}
        className={className}
        role='status'
      />
    </span>
  )
}
