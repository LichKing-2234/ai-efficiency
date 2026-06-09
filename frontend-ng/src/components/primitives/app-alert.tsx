import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

export function AppAlert({
  title,
  description,
  tone = 'info'
}: {
  title: string
  description?: string
  tone?: 'info' | 'success' | 'warning' | 'error'
}) {
  return (
    <Alert variant={tone === 'error' ? 'destructive' : 'default'} data-tone={tone}>
      <AlertTitle>{title}</AlertTitle>
      {description ? <AlertDescription>{description}</AlertDescription> : null}
    </Alert>
  )
}
