import { KeyRound, Layers, Waypoints } from 'lucide-react'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { useI18n } from '@/lib/i18n/i18n'
import type { SettingsSection } from './settings-payloads'

const settingsSectionAddIcons = {
  'ai-services': Layers,
  'code-platforms': Waypoints,
  'advanced-credentials': KeyRound
} as const satisfies Record<'ai-services' | 'code-platforms' | 'advanced-credentials', typeof Layers>

const settingsSectionAddLabels = {
  'ai-services': 'settings.addRelayProvider',
  'code-platforms': 'settings.addScmProvider',
  'advanced-credentials': 'settings.addCredential'
} as const

export function SettingsSectionAddAction({
  onClick,
  section
}: {
  onClick: () => void
  section: Extract<SettingsSection, 'ai-services' | 'code-platforms' | 'advanced-credentials'>
}) {
  const { t } = useI18n()
  const icon = settingsSectionAddIcons[section]
  const label = settingsSectionAddLabels[section]

  return (
    <ButtonWithIcon size='sm' icon={icon} onClick={onClick}>
      {t(label)}
    </ButtonWithIcon>
  )
}
