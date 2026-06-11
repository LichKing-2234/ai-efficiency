import type * as React from 'react'
import { cn } from '@/lib/utils'

function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  return (
    <textarea
      data-slot='textarea'
      className={cn(
        'flex min-h-24 w-full rounded-[var(--r-md)] border border-input bg-[var(--surface-inset)] px-3.5 py-2.5 text-[13px] shadow-none outline-none transition-[border-color,background-color] duration-150 ease-[var(--ease-out)] placeholder:text-muted-foreground focus-visible:border-ring disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
      {...props}
    />
  )
}

export { Textarea }
