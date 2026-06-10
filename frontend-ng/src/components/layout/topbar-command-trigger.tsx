import { SearchIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function TopbarCommandTrigger({
  label,
  onOpen
}: {
  label: string
  onOpen: () => void
}) {
  return (
    <span className='contents' data-slot='topbar-command-trigger'>
      <Button
        className='hidden min-w-48 justify-start gap-2 text-[var(--ink-3)] lg:inline-flex'
        data-slot='topbar-command-trigger-desktop'
        onClick={onOpen}
        size='sm'
        type='button'
        variant='outline'
      >
        <SearchIcon />
        <span className='flex-1 text-left'>{label}</span>
        <kbd
          className='rounded border border-border bg-[var(--surface)] px-1.5 py-0.5 font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'
          data-slot='topbar-command-trigger-kbd'
        >
          ⌘K
        </kbd>
      </Button>
      <Button
        className='lg:hidden'
        data-slot='topbar-command-trigger-mobile'
        onClick={onOpen}
        size='icon-sm'
        title={label}
        type='button'
        variant='outline'
      >
        <SearchIcon />
      </Button>
    </span>
  )
}
