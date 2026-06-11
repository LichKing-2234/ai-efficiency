import { Command as CommandPrimitive } from 'cmdk'
import { CheckIcon, SearchIcon } from 'lucide-react'
import * as React from 'react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { InputGroup, InputGroupAddon } from '@/components/ui/input-group'
import { cn } from '@/lib/utils'

function Command({ className, ...props }: React.ComponentProps<typeof CommandPrimitive>) {
  return (
    <CommandPrimitive
      data-slot='command'
      className={cn('flex size-full flex-col overflow-hidden rounded-[var(--r-lg)] bg-popover p-1 text-popover-foreground', className)}
      {...props}
    />
  )
}

function CommandDialog({
  title = 'Command Palette',
  description = 'Search for a command to run.',
  children,
  className,
  showCloseButton = false,
  ...props
}: React.ComponentProps<typeof Dialog> & {
  title?: string
  description?: string
  className?: string
  showCloseButton?: boolean
}) {
  return (
    <Dialog {...props}>
      <DialogContent
        className={cn(
          'top-[13vh] max-h-[min(640px,74vh)] w-[min(580px,calc(100vw-32px))] translate-y-0 overflow-hidden rounded-[var(--r-lg)] border-[var(--line-strong)] bg-popover p-0',
          className
        )}
        showCloseButton={showCloseButton}
      >
        <DialogHeader className='sr-only'>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        {children}
      </DialogContent>
    </Dialog>
  )
}

function CommandInput({ className, ...props }: React.ComponentProps<typeof CommandPrimitive.Input>) {
  return (
    <div data-slot='command-input-wrapper' className='border-b border-border p-2'>
      <InputGroup className='h-10 rounded-[var(--r-sm)] border-transparent bg-transparent shadow-none has-[[data-slot=input-group-control]:focus-visible]:border-transparent has-[[data-slot=input-group-control]:focus-visible]:ring-0'>
        <InputGroupAddon>
          <SearchIcon className='text-[var(--ink-3)]' />
        </InputGroupAddon>
        <CommandPrimitive.Input
          data-slot='command-input'
          className={cn('min-w-0 flex-1 bg-transparent text-[15px] outline-none placeholder:text-[var(--ink-3)] disabled:cursor-not-allowed disabled:opacity-50', className)}
          {...props}
        />
      </InputGroup>
    </div>
  )
}

function CommandList({ className, ...props }: React.ComponentProps<typeof CommandPrimitive.List>) {
  return (
    <CommandPrimitive.List
      data-slot='command-list'
      className={cn('max-h-[52vh] min-h-0 scroll-py-1 overflow-x-hidden overflow-y-auto p-2 outline-none', className)}
      {...props}
    />
  )
}

function CommandEmpty({ className, ...props }: React.ComponentProps<typeof CommandPrimitive.Empty>) {
  return (
    <CommandPrimitive.Empty
      data-slot='command-empty'
      className={cn('px-4 py-8 text-center text-[12px] text-[var(--ink-3)]', className)}
      {...props}
    />
  )
}

function CommandGroup({ className, ...props }: React.ComponentProps<typeof CommandPrimitive.Group>) {
  return (
    <CommandPrimitive.Group
      data-slot='command-group'
      className={cn(
        "overflow-hidden p-1 text-foreground [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1 [&_[cmdk-group-heading]]:font-bold [&_[cmdk-group-heading]]:text-[10px] [&_[cmdk-group-heading]]:text-[var(--ink-4)] [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-[0.07em]",
        className
      )}
      {...props}
    />
  )
}

function CommandSeparator({ className, ...props }: React.ComponentProps<typeof CommandPrimitive.Separator>) {
  return <CommandPrimitive.Separator data-slot='command-separator' className={cn('-mx-1 h-px bg-border', className)} {...props} />
}

function CommandItem({ className, children, ...props }: React.ComponentProps<typeof CommandPrimitive.Item>) {
  return (
    <CommandPrimitive.Item
      data-slot='command-item'
      className={cn(
        "group/command-item relative flex h-9 cursor-default select-none items-center gap-3 rounded-[var(--r-sm)] px-2.5 text-[13px] text-[var(--ink-2)] outline-none transition-colors data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-50 data-[selected=true]:bg-[var(--ai-soft)] data-[selected=true]:text-foreground [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4 data-[selected=true]:[&_svg]:text-[var(--ai-deep)]",
        className
      )}
      {...props}
    >
      {children}
      <CheckIcon className='ml-auto opacity-0 group-data-[checked=true]/command-item:opacity-100 group-has-[[data-slot=command-shortcut]]/command-item:hidden' />
    </CommandPrimitive.Item>
  )
}

function CommandShortcut({ className, ...props }: React.ComponentProps<'span'>) {
  return (
    <span
      data-slot='command-shortcut'
      className={cn('ml-auto text-[11px] text-[var(--ink-4)] group-data-[selected=true]/command-item:text-[var(--ai-deep)]', className)}
      {...props}
    />
  )
}

export { Command, CommandDialog, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem, CommandShortcut, CommandSeparator }
