import { createFileRoute } from '@tanstack/react-router'
import { OAuthAuthorizePage } from '@/features/oauth/oauth-pages'

export const Route = createFileRoute('/oauth/authorize')({
  component: OAuthAuthorizePage
})
