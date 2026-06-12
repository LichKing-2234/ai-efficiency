import type * as React from 'react'
import { Button } from '@/components/ui/button'

export function SecondaryActionButton(props: Omit<React.ComponentProps<typeof Button>, 'variant'>) {
  return <Button variant='outline' {...props} />
}
