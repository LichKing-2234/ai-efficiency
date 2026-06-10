import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export function LoadingState({ label = 'Loading...' }: { label?: string }) {
  return (
    <Card data-slot='data-state-loading'>
      <CardContent className='p-6 text-muted-foreground text-sm' data-slot='data-state-loading-content'>{label}</CardContent>
    </Card>
  )
}

export function EmptyState({ title, description, action }: { title: string; description?: string; action?: React.ReactNode }) {
  return (
    <Card data-slot='data-state-empty'>
      <CardContent className='flex flex-col items-center gap-3 p-8 text-center'>
        <div className='font-medium' data-slot='data-state-empty-title'>{title}</div>
        {description ? <div className='max-w-md text-muted-foreground text-sm' data-slot='data-state-empty-description'>{description}</div> : null}
        {action}
      </CardContent>
    </Card>
  )
}

export function ErrorState({ message, onRetry, retryLabel = 'Retry' }: { message: string; onRetry?: () => void; retryLabel?: string }) {
  return (
    <Card className='border-[var(--ae-warn-soft)]' data-slot='data-state-error'>
      <CardContent className='flex items-center justify-between gap-3 p-5 text-sm' data-slot='data-state-error-content'>
        <span className='text-[var(--ae-warn)]' data-slot='data-state-error-message'>{message}</span>
        {onRetry ? (
          <Button variant='outline' size='sm' onClick={onRetry}>
            {retryLabel}
          </Button>
        ) : null}
      </CardContent>
    </Card>
  )
}
