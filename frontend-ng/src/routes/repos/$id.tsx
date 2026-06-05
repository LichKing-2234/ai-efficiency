import { createFileRoute } from '@tanstack/react-router'
import { RepoDetailPage } from '@/features/repos/repo-detail-page'

export const Route = createFileRoute('/repos/$id')({
  component: RepoDetailPage
})
