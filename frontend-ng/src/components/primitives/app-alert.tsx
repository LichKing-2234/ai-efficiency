import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

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
      {actions ? <div data-slot='app-alert-actions' className='mt-3'>{actions}</div> : null}
    </Alert>
  )
}
