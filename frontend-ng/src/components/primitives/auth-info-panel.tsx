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
        'px-[12px] py-[10px] text-[11.5px] leading-[1.5] text-[var(--ink-3)]',
        emphasis && 'border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]',
        className
      )}
      dataSlot='auth-info-panel'
    >
      {children}
    </InsetPanel>
  )
}
