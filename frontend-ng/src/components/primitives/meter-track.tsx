import { cn } from '@/lib/utils'

export function MeterTrack({
  children,
  className,
  dataSlot = 'meter-track'
}: {
  children: React.ReactNode
  className?: string
  dataSlot?: string
}) {
  return (
    <span className={cn('h-1.5 max-w-[88px] flex-1 overflow-hidden rounded-full bg-[var(--surface-inset)]', className)} data-slot={dataSlot}>
      {children}
    </span>
  )
}
