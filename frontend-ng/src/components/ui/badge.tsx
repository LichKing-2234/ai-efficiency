import { cva, type VariantProps } from 'class-variance-authority'
import type * as React from 'react'
import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex w-fit shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 font-medium text-[11px]',
  {
    variants: {
      variant: {
        default: 'border-transparent bg-primary text-primary-foreground',
        secondary: 'border-border bg-secondary text-secondary-foreground',
        outline: 'border-border text-foreground',
        success: 'border-transparent bg-[var(--ae-pos-soft)] text-[var(--ae-pos)]',
        warning: 'border-transparent bg-[var(--ae-warn-soft)] text-[var(--ae-warn)]',
        ai: 'border-[var(--ae-ai-line)] bg-[var(--ae-ai-soft)] text-[var(--ae-ai-2)]'
      }
    },
    defaultVariants: {
      variant: 'secondary'
    }
  }
)

function Badge({ className, variant, ...props }: React.ComponentProps<'span'> & VariantProps<typeof badgeVariants>) {
  return <span data-slot='badge' className={cn(badgeVariants({ variant, className }))} {...props} />
}

export { Badge, badgeVariants }
