import { cn } from '@/lib/utils'

export function identityInitials(value: string) {
  const parts = value.trim().split(/[\s._@-]+/).filter(Boolean)
  const initials = parts.slice(0, 2).map((part) => part[0]?.toUpperCase()).join('')
  return initials || '?'
}

export function IdentityAvatar({
  className,
  value
}: {
  className?: string
  value: string
}) {
  return (
    <span
      className={cn('grid size-8 shrink-0 place-items-center rounded-full bg-[var(--surface-3)] font-bold text-[11px] text-[var(--ink-2)]', className)}
      data-slot='identity-avatar'
    >
      {identityInitials(value)}
    </span>
  )
}
