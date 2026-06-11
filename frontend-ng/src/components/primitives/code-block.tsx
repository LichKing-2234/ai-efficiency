import { cn } from '@/lib/utils'

export function CodeBlock({
  ariaLabel,
  children,
  className
}: {
  ariaLabel?: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <pre
      aria-label={ariaLabel}
      className={cn('max-h-56 overflow-auto rounded-[var(--r-md)] bg-[var(--surface-inset)] p-[14px] text-[12px] leading-5 text-[var(--ink-2)]', className)}
      data-slot='code-block'
    >
      {children}
    </pre>
  )
}
