import { Stack } from '@/components/primitives/stack'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

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
  return (
    <Alert variant={tone === 'error' ? 'destructive' : 'default'} data-tone={tone}>
      <AlertTitle>{title}</AlertTitle>
      {description ? <AlertDescription>{description}</AlertDescription> : null}
      {actions ? <AppAlertActions>{actions}</AppAlertActions> : null}
    </Alert>
  )
}
