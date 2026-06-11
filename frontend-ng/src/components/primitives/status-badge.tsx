import { Badge } from '@/components/ui/badge'

export function StatusBadge({ value }: { value?: string | null }) {
  const text = value || 'unknown'
  const variant =
    ['active', 'healthy', 'fresh', 'completed', 'success', 'bound', 'admin'].includes(text)
      ? 'success'
      : ['running', 'queued', 'pending_upload', 'review', 'user', 'invited', 'syncing'].includes(text)
        ? 'ai'
        : ['failed', 'abandoned', 'unbound', 'missing', 'refresh_failed', 'suspended', 'error'].includes(text)
          ? 'neg'
          : 'secondary'
  return <Badge variant={variant}>{text.replaceAll('_', ' ')}</Badge>
}
