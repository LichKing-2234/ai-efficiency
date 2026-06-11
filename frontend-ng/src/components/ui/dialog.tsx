import { Dialog as DialogPrimitive } from 'radix-ui'
import type * as React from 'react'
import { XIcon } from 'lucide-react'
import { cn } from '@/lib/utils'

const Dialog = DialogPrimitive.Root
const DialogTrigger = DialogPrimitive.Trigger
const DialogClose = DialogPrimitive.Close

function DialogContent({
  className,
  children,
  showCloseButton = true,
  ...props
}: React.ComponentProps<typeof DialogPrimitive.Content> & {
  showCloseButton?: boolean
}) {
  return (
    <DialogPrimitive.Portal>
      <DialogPrimitive.Overlay className='fixed inset-0 z-50 bg-black/35 backdrop-blur-sm' />
      <DialogPrimitive.Content
        className={cn(
          'fixed left-1/2 top-[13vh] z-50 grid w-[min(580px,92vw)] -translate-x-1/2 gap-4 rounded-[var(--r-lg)] border border-border bg-[var(--surface)] p-[18px] data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95 data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=closed]:zoom-out-95',
          className
        )}
        {...props}
      >
        {children}
        {showCloseButton ? (
          <DialogPrimitive.Close className='absolute right-3 top-3 rounded-[var(--r-sm)] p-1 text-[var(--ink-3)] hover:bg-[var(--surface-2)] hover:text-foreground'>
            <XIcon />
            <span className='sr-only'>Close</span>
          </DialogPrimitive.Close>
        ) : null}
      </DialogPrimitive.Content>
    </DialogPrimitive.Portal>
  )
}

function DialogHeader(props: React.ComponentProps<'div'>) {
  return <div className={cn('flex flex-col gap-1.5', props.className)} {...props} />
}

function DialogTitle({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Title>) {
  return <DialogPrimitive.Title className={cn('font-[650] text-[14px] text-[var(--ink)]', className)} {...props} />
}

function DialogDescription({ className, ...props }: React.ComponentProps<typeof DialogPrimitive.Description>) {
  return <DialogPrimitive.Description className={cn('text-[12px] text-[var(--ink-3)]', className)} {...props} />
}

export { Dialog, DialogTrigger, DialogClose, DialogContent, DialogHeader, DialogTitle, DialogDescription }
