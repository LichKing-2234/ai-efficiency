import { ActionGroup } from '@/components/primitives/action-group'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { Stack } from '@/components/primitives/stack'
import { useI18n } from '@/lib/i18n/i18n'

export function LoadingState({ label = 'Loading...' }: { label?: string }) {
  return (
    <Card data-slot='data-state-loading'>
      <CardContentStack className='p-[18px]' dataSlot='data-state-loading-content'>
        <Stack className='items-center text-center text-[12px] text-[var(--ink-3)]' dataSlot='data-state-loading-copy' gap='compact'>
          <div className='font-medium text-foreground/80' data-slot='data-state-loading-label'>{label}</div>
        </Stack>
      </CardContentStack>
    </Card>
  )
}

export function EmptyState({ title, description, action }: { title: string; description?: string; action?: React.ReactNode }) {
  return (
    <Card data-slot='data-state-empty'>
      <CardContentStack className='p-[24px]' dataSlot='data-state-empty-content'>
        <Empty>
          <EmptyHeader>
            <EmptyTitle>{title}</EmptyTitle>
            {description ? <EmptyDescription>{description}</EmptyDescription> : null}
          </EmptyHeader>
          {action ? <EmptyContent>{action}</EmptyContent> : null}
        </Empty>
      </CardContentStack>
    </Card>
  )
}

export function ErrorState({ message, onRetry, retryLabel = 'Retry' }: { message: string; onRetry?: () => void; retryLabel?: string }) {
  const { t } = useI18n()
  return (
    <Card className='bg-[var(--surface)] border-[var(--line-strong)]' data-slot='data-state-error'>
      <CardContentStack className='p-[14px] text-[12px]' dataSlot='data-state-error-content'>
        <ActionGroup align='responsive-end' dataSlot='data-state-error-row' fit layout='split' wrap>
          <Stack className='min-w-0' dataSlot='data-state-error-copy' gap='compact'>
            <div className='font-medium text-[var(--neg)]' data-slot='data-state-error-title'>
              {t('common.requestFailed')}
            </div>
            <span className='text-[var(--ink-3)]' data-slot='data-state-error-message'>{message}</span>
          </Stack>
          {onRetry ? (
            <Button variant='outline' size='sm' onClick={onRetry}>
              {retryLabel}
            </Button>
          ) : null}
        </ActionGroup>
      </CardContentStack>
    </Card>
  )
}
