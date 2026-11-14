import { Button, Segmented } from '@/components/ui/primitives'
import { useSecurityDialogs } from '@/components/SecurityModals'
import { Group, SettingRow } from '@/features/settings/components/SettingsParts'
import { ROLE_LABEL } from '@/lib/api'
import { getThemePreference, setThemePreference, type ThemePreference } from '@/lib/theme'
import { useServer } from '@/store/server'
import { useState } from 'react'

const THEMES: { value: ThemePreference; label: string }[] = [
  { value: 'system', label: 'System' },
  { value: 'light', label: 'Light' },
  { value: 'dark', label: 'Dark' },
]

/** What a person owns about themselves: how the interface looks to them, and how they sign in. It used to be a list in the sidebar's menu. */
export default function AccountSettings() {
  const user = useServer((s) => s.user)
  const role = useServer((s) => s.role)
  const security = useSecurityDialogs()
  const [theme, setTheme] = useState<ThemePreference>(getThemePreference)
  if (!user) return null
  const methods = [user.twoFactorEnabled && 'Authenticator app', user.emailOtpEnabled && 'Email code', user.passkeys.length > 0 && (user.passkeys.length === 1 ? '1 passkey' : `${user.passkeys.length} passkeys`)].filter(Boolean)

  return (
    <div className="space-y-6" data-testid="settings-account">
      <Group title="You" id="you">
        <SettingRow label="Signed in as" value={`${user.username}${role ? ` · ${ROLE_LABEL[role]}` : ''}`} valueTestId="settings-who" />
        <SettingRow
          label="Appearance"
          value={<Segmented label="Theme" value={theme} options={THEMES} onChange={(v) => { setThemePreference(v); setTheme(v) }} />}
        />
      </Group>
      <Group title="Sign-in" id="sign-in">
        <SettingRow label="Password" value="Set" action={<Button size="sm" onClick={security.open.password} data-testid="change-password-open">Change</Button>} />
        <SettingRow
          label="Two-factor"
          tone={methods.length ? 'healthy' : 'off'}
          value={methods.length ? methods.join(', ') : 'Off'}
          valueTestId="settings-two-factor"
          action={<Button size="sm" onClick={security.open.twoFactor} data-testid="two-factor-open">Manage</Button>}
        />
        <SettingRow label="API tokens" value="For scripts and the API" action={<Button size="sm" onClick={security.open.tokens} data-testid="api-tokens-open">Manage</Button>} />
      </Group>
      {security.dialogs}
    </div>
  )
}
