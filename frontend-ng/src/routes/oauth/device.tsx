import { createFileRoute } from '@tanstack/react-router'
import { OAuthDevicePage } from '@/features/oauth/oauth-pages'

export const Route = createFileRoute('/oauth/device')({
  component: OAuthDevicePage
})
