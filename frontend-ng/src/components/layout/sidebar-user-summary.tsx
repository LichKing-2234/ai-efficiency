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
    <div className={cn('flex items-center gap-[9px] rounded-[var(--r-md)] border border-[var(--line)] bg-sidebar-accent p-[7px]', compact && 'size-[42px] justify-center rounded-[var(--r-sm)] border-transparent bg-transparent p-0')} data-slot='sidebar-user-summary'>
      <IdentityAvatar className='bg-[var(--ae-ai-soft)] text-[var(--ae-ai-2)]' value={identity} />
      {!compact ? (
        <div className='min-w-0 flex-1'>
          <div className='truncate font-semibold text-[12.5px]' data-slot='sidebar-user-summary-name'>
            {user?.username || fallbackName}
          </div>
          <div className='truncate text-[10.5px] text-[var(--ink-4)]' data-slot='sidebar-user-summary-role'>
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
