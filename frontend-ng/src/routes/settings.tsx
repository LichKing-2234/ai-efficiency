import { createFileRoute } from '@tanstack/react-router'
import { SettingsPage, settingsRouteGuard } from '@/features/settings/settings-page'

export const Route = createFileRoute('/settings')({
  beforeLoad: settingsRouteGuard,
  component: SettingsPage
})
