import clsx from 'clsx'
import { Check, Minus, X } from 'lucide-react'
import { ICON_SM } from '@/components/ui/primitives'

/** The rules the server enforces (see backend/internal/server/password.go's CheckPasswordPolicy):
 *  length first, then a digit and a symbol on top - not NIST's composition-free recommendation, but a
 *  deliberate exception to it (see that function's own doc comment for why). */
export function passwordRules(password: string, username: string) {
  const repeatedChar = password.length > 0 && [...password].every((c) => c === password[0])
  const hasDigit = /\d/u.test(password)
  const hasSymbol = [...password].some((c) => !/[\p{L}\d]/u.test(c))
  return [
    { key: 'length', label: '12–128 characters', ok: password.length >= 12 && password.length <= 128 },
    { key: 'username', label: 'Not the same as your username', ok: password.length > 0 && password.toLowerCase() !== username.trim().toLowerCase() },
    { key: 'repeat', label: 'Not a single character repeated', ok: password.length > 0 && !repeatedChar },
    { key: 'digit', label: 'At least one number', ok: hasDigit },
    { key: 'symbol', label: 'At least one symbol (e.g. ! @ # $ %)', ok: hasSymbol },
  ]
}

/**
 * Live checklist of the password policy, one line per rule: gray and unmarked before the person has
 * typed anything, then green or red as it starts matching or missing each one. Shared between sign-up
 * and the forced change-password screen, the only two places someone picks a new password.
 */
export function PasswordRequirements({ password, username }: { password: string; username: string }) {
  const empty = password === ''
  return (
    <ul className="space-y-1">
      {passwordRules(password, username).map((r) => (
        <li key={r.key} className={clsx('flex items-center gap-1.5 text-xs', empty ? 'text-nb-500' : r.ok ? 'text-ok' : 'text-bad')}>
          {empty ? <Minus size={ICON_SM} aria-hidden /> : r.ok ? <Check size={ICON_SM} className="fade-in" aria-hidden /> : <X size={ICON_SM} className="fade-in" aria-hidden />}
          {r.label}
          {!empty && <span className="sr-only">{r.ok ? ' satisfied' : ' not satisfied'}</span>}
        </li>
      ))}
    </ul>
  )
}
