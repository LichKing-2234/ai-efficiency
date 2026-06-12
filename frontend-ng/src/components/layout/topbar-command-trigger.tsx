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
        className='cmd-trigger hidden h-[34px] min-w-[188px] justify-start gap-[9px] border-[var(--line)] bg-[var(--surface-inset)] px-[10px] pl-[11px] text-[12.5px] text-[var(--ink-3)] min-[920px]:inline-flex'
        data-slot='topbar-command-trigger-desktop'
        onClick={onOpen}
        size='default'
        type='button'
        variant='outline'
      >
        <SearchIcon />
        <span className='flex-1 text-left'>{label}</span>
        <kbd
          className='rounded-[5px] border border-[var(--line)] bg-[var(--surface)] px-[6px] py-[2px] font-mono font-semibold text-[10.5px] text-[var(--ink-3)]'
          data-slot='topbar-command-trigger-kbd'
        >
          ⌘K
        </kbd>
      </Button>
      <Button
        className='min-[920px]:hidden'
        data-slot='topbar-command-trigger-mobile'
        onClick={onOpen}
        size='icon'
        title={label}
        type='button'
        variant='outline'
      >
        <SearchIcon />
      </Button>
    </span>
  )
}
