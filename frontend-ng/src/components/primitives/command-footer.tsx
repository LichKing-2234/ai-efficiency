import type * as React from 'react'
import { cn } from '@/lib/utils'

function CommandFooterKey({ children }: { children: React.ReactNode }) {
  return (
    <kbd
      className='rounded-[5px] border border-[var(--line)] bg-[var(--surface)] px-[6px] py-[2px] font-[var(--font-mono)] text-[10.5px] font-semibold text-[var(--ink-3)]'
      data-slot='command-footer-key'
    >
      {children}
    </kbd>
  )
}

function CommandFooterHint({
  keys,
  label
}: {
  keys: React.ReactNode
  label: React.ReactNode
}) {
  return (
    <span className='flex items-center gap-[5px]' data-slot='command-footer-hint'>
      <span className='flex items-center gap-[3px]'>{keys}</span>
      <span>{label}</span>
    </span>
  )
}

export function CommandFooter({
  children,
  className
}: {
  children: React.ReactNode
  className?: string
}) {
  return (
    <div
      data-slot='command-footer'
      className={cn('flex items-center gap-[14px] border-t border-border px-4 py-2.5 text-[11px] text-[var(--ink-4)]', className)}
    >
      {children}
    </div>
  )
}

CommandFooter.Key = CommandFooterKey
CommandFooter.Hint = CommandFooterHint
