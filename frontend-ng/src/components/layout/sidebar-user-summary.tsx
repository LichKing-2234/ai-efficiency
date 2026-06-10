import { LogOutIcon } from 'lucide-react'
import { IdentityAvatar } from '@/components/primitives/identity-avatar'
import { Button } from '@/components/ui/button'
import type { User } from '@/lib/api/types'
import { cn } from '@/lib/utils'

export function SidebarUserSummary({
  compact,
  fallbackName,
  fallbackRole,
  onSignOut,
  signOutLabel,
  user
}: {
  compact: boolean
  fallbackName: string
  fallbackRole: string
  onSignOut: () => void
  signOutLabel: string
  user: User | null
}) {
  const identity = user?.username || user?.email || fallbackName

  return (
    <div className={cn('flex items-center gap-2', compact && 'justify-center')} data-slot='sidebar-user-summary'>
      <IdentityAvatar className='bg-[var(--ae-ai-soft)] text-[var(--ae-ai-2)]' value={identity} />
      {!compact ? (
        <div className='min-w-0 flex-1'>
          <div className='truncate font-medium text-sm' data-slot='sidebar-user-summary-name'>
            {user?.username || fallbackName}
          </div>
          <div className='truncate text-muted-foreground text-xs' data-slot='sidebar-user-summary-role'>
            {user?.role || fallbackRole}
          </div>
        </div>
      ) : null}
      {!compact ? (
        <Button aria-label={signOutLabel} onClick={onSignOut} size='icon-sm' title={signOutLabel} type='button' variant='ghost'>
          <LogOutIcon />
        </Button>
      ) : null}
    </div>
  )
}
