import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'

export function LoadingState({ label = 'Loading...' }: { label?: string }) {
  return (
    <Card>
      <CardContent className='p-6 text-muted-foreground text-sm'>{label}</CardContent>
    </Card>
  )
}

export function EmptyState({ title, description, action }: { title: string; description?: string; action?: React.ReactNode }) {
  return (
    <Card>
      <CardContent className='flex flex-col items-center gap-3 p-8 text-center'>
        <div className='font-medium'>{title}</div>
        {description ? <div className='max-w-md text-muted-foreground text-sm'>{description}</div> : null}
        {action}
      </CardContent>
    </Card>
  )
}

export function ErrorState({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <Card className='border-[var(--ae-warn-soft)]'>
      <CardContent className='flex items-center justify-between gap-3 p-5 text-sm'>
        <span className='text-[var(--ae-warn)]'>{message}</span>
        {onRetry ? (
          <Button variant='outline' size='sm' onClick={onRetry}>
            Retry
          </Button>
        ) : null}
      </CardContent>
    </Card>
  )
}
