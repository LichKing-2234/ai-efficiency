import type * as React from 'react'
import { cn } from '@/lib/utils'

function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return (
    <input
      type={type}
      data-slot='input'
      className={cn(
        'flex h-10 w-full rounded-[var(--r-md)] border border-input bg-[var(--surface-inset)] px-3.5 py-2 text-[13px] shadow-none outline-none transition-[border-color,background-color] duration-150 ease-[var(--ease-out)] placeholder:text-muted-foreground focus-visible:border-ring disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
      {...props}
    />
  )
}

export { Input }
