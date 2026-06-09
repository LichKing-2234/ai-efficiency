import { cn } from '@/lib/utils'

export function SectionEyebrow({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('mb-2.5 font-bold text-[11px] text-[var(--ink-4)] uppercase tracking-[0.06em]', className)} data-slot='section-eyebrow'>
      {children}
    </div>
  )
}
