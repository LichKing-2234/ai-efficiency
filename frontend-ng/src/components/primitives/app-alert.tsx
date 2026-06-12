import { Stack } from '@/components/primitives/stack'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { cn } from '@/lib/utils'

function AppAlertActions({ children }: { children: React.ReactNode }) {
  return (
    <Stack className='mt-3' dataSlot='app-alert-actions' gap='none'>
      {children}
    </Stack>
  )
}

export function AppAlert({
  actions,
  title,
  description,
  tone = 'info'
}: {
  actions?: React.ReactNode
  title: string
  description?: string
  tone?: 'info' | 'success' | 'warning' | 'error'
}) {
  const toneClassName = {
    info: '',
    success: 'border-[var(--pos-line)] bg-[var(--pos-soft)] text-[var(--pos)] [&_[data-slot=alert-description]]:text-[var(--pos)]/85',
    warning: 'border-[var(--warn-line)] bg-[var(--warn-soft)] text-[var(--warn)] [&_[data-slot=alert-description]]:text-[var(--warn)]/88',
    error: ''
  } satisfies Record<typeof tone, string>

  return (
    <Alert
      className={cn(toneClassName[tone])}
      data-tone={tone}
      variant={tone === 'error' ? 'destructive' : 'default'}
    >
      <AlertTitle>{title}</AlertTitle>
      {description ? <AlertDescription>{description}</AlertDescription> : null}
      {actions ? <AppAlertActions>{actions}</AppAlertActions> : null}
    </Alert>
  )
}
