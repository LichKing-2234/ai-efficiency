import type { LucideIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

export function EntityGlyph({
  className,
  icon: Icon,
  label
}: {
  className?: string
  icon: LucideIcon
  label: string
}) {
  return (
    <span
      aria-label={label}
      className={cn('grid size-9 shrink-0 place-items-center rounded-[var(--r-md)] border border-[var(--ai-line)] bg-[var(--ai-soft)] text-[var(--ai-deep)]', className)}
      data-slot='entity-glyph'
    >
      <Icon />
    </span>
  )
}
