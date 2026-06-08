import type * as React from 'react'
import { cn } from '@/lib/utils'

function Input({ className, type, ...props }: React.ComponentProps<'input'>) {
  return (
    <input
      type={type}
      data-slot='input'
      className={cn(
        'flex h-8 w-full rounded-md border border-input bg-card px-3 py-1 text-sm outline-none transition-colors placeholder:text-muted-foreground focus-visible:ring-[3px] focus-visible:ring-ring/30 disabled:cursor-not-allowed disabled:opacity-50',
        className
      )}
      {...props}
    />
  )
}

export { Input }
