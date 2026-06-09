import { InsetPanel } from '@/components/primitives/inset-panel'
import { cn } from '@/lib/utils'

export function AuthInfoPanel({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <InsetPanel className={cn('text-muted-foreground', className)} dataSlot='auth-info-panel'>
      {children}
    </InsetPanel>
  )
}
