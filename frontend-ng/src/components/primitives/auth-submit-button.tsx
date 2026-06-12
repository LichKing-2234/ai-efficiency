import type * as React from 'react'
import { Button } from '@/components/ui/button'

export function AuthSubmitButton(props: React.ComponentProps<typeof Button>) {
  return <Button className='w-full' {...props} />
}
