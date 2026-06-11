import { InsetPanel } from '@/components/primitives/inset-panel'
import { cn } from '@/lib/utils'

export function AuthInfoPanel({
  children,
  className,
  emphasis = false
}: {
  children: React.ReactNode
  className?: string
  emphasis?: boolean
}) {
  return (
    <InsetPanel
      className={cn(
        'text-[12px] text-[var(--ink-3)]',
        emphasis && 'border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]',
        className
      )}
      dataSlot='auth-info-panel'
    >
      {children}
    </InsetPanel>
  )
}
