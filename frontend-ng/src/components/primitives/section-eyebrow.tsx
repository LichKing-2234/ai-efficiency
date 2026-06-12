import { cn } from '@/lib/utils'

type SectionEyebrowProps = React.HTMLAttributes<HTMLDivElement>

export function SectionEyebrow({
  children,
  className,
  ...props
}: SectionEyebrowProps) {
  return (
    <div
      className={cn('mb-2.5 font-bold text-[11px] text-[var(--ink-4)] uppercase tracking-[0.06em]', className)}
      data-slot='section-eyebrow'
      {...props}
    >
      {children}
    </div>
  )
}
