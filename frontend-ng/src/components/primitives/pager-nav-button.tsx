import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function PagerNavButton({
  direction,
  children,
  ...props
}: Omit<React.ComponentProps<typeof Button>, 'size' | 'variant'> & {
  direction: 'next' | 'previous'
}) {
  return (
    <Button size='sm' variant='outline' {...props}>
      {direction === 'previous' ? <ChevronLeftIcon data-icon='inline-start' /> : null}
      {children}
      {direction === 'next' ? <ChevronRightIcon data-icon='inline-end' /> : null}
    </Button>
  )
}
