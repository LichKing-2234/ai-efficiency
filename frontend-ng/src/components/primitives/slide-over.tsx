import { XIcon } from 'lucide-react'
import { useEffect } from 'react'
import { ActionGroup } from '@/components/primitives/action-group'
import { SlideOverStack } from '@/components/primitives/slide-over-stack'
import { Stack } from '@/components/primitives/stack'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

function SlideOverBody({ children }: { children: React.ReactNode }) {
  return (
    <SlideOverStack className='min-h-0 flex-1 overflow-y-auto p-[18px]'>{children}</SlideOverStack>
  )
}

export function SlideOver({
  open,
  title,
  subtitle,
  leading,
  children,
  onClose,
  className
}: {
  open: boolean
  title: React.ReactNode
  subtitle?: React.ReactNode
  leading?: React.ReactNode
  children: React.ReactNode
  onClose: () => void
  className?: string
}) {
  useEffect(() => {
    if (!open) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose, open])

  if (!open) return null

  return (
    <div
      className='fixed inset-0 z-50 flex justify-end bg-[color-mix(in_oklab,var(--bg-sunken)_45%,transparent)] backdrop-blur-[2px]'
      onClick={onClose}
    >
      <section
        aria-modal='true'
        className={cn(
          'flex h-full w-[min(500px,94vw)] flex-col overflow-hidden border-l border-[var(--line-strong)] bg-[var(--surface)]',
          'motion-safe:animate-[slideover-in_.26s_var(--ease-out)_both]',
          className
        )}
        onClick={(event) => event.stopPropagation()}
        role='dialog'
      >
        <ActionGroup
          align='start'
          className='sticky top-0 z-10 border-b border-border bg-[color-mix(in_oklab,var(--surface)_88%,transparent)] px-[18px] py-4 backdrop-blur'
          dataSlot='slide-over-header'
          fit
        >
          {leading}
          <Stack className='min-w-0 flex-1' dataSlot='slide-over-header-copy' gap='none'>
            <h2 className='truncate font-semibold text-[14.5px]'>{title}</h2>
            {subtitle ? <div className='truncate text-[11.5px] text-[var(--ink-3)]'>{subtitle}</div> : null}
          </Stack>
          <Button aria-label='Close' onClick={onClose} size='icon' type='button' variant='ghost'>
            <XIcon />
          </Button>
        </ActionGroup>
        <SlideOverBody>{children}</SlideOverBody>
      </section>
    </div>
  )
}
