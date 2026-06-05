import { createFileRoute } from '@tanstack/react-router'
import { ReposPage } from '@/features/repos/repos-page'

export const Route = createFileRoute('/repos')({
  component: ReposPage
})
