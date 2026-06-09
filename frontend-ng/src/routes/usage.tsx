import { createFileRoute } from '@tanstack/react-router'
import { UsagePage } from '@/features/user-usage/usage-page'

export const Route = createFileRoute('/usage')({
  component: UsagePage
})
