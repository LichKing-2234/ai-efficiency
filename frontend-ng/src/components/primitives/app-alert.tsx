import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

const appAlertActionsClass = 'mt-3'

function AppAlertActions({ children }: { children: React.ReactNode }) {
  return (
    <div className={appAlertActionsClass} data-slot='app-alert-actions'>
      {children}
    </div>
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
