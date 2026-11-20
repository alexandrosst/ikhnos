import { describe, expect, test } from 'vitest'
import { HEALTH, inWords, msUntil, shortAge, tierOf } from '@/features/agents/lib/agentView'
import { HEALTH_ORDER } from '@/features/agents/lib/agentHealth'
import type { Agent } from '@/lib/types'

const H = 3_600_000

describe('agentView', () => {
  test('a certificate lifetime is said in the unit that is honest for it: minutes, hours, then days', () => {
    expect(inWords(10 * 60_000)).toBe('10 min')
    expect(inWords(1_000)).toBe('1 min') // never "0 min"
    expect(inWords(5 * H)).toBe('5 h')
    expect(inWords(47 * H)).toBe('47 h')
    expect(inWords(72 * H)).toBe('3 d')
  })
  test('a short age is for places with no room for a sentence', () => {
    const now = Date.parse('2026-01-01T12:00:00Z')
    const ago = (ms: number) => new Date(now - ms).toISOString()
    expect(shortAge(ago(30_000), now)).toBe('now')
    expect(shortAge(ago(10 * 60_000), now)).toBe('10m ago')
    expect(shortAge(ago(3 * H), now)).toBe('3h ago')
    expect(shortAge(ago(50 * H), now)).toBe('2d ago')
  })
  test('time until a certificate expires is negative once it has, and undefined when there is none', () => {
    const now = Date.parse('2026-01-01T12:00:00Z')
    expect(msUntil('2026-01-01T14:00:00Z', now)).toBe(2 * H)
    expect(msUntil('2026-01-01T11:00:00Z', now)).toBe(-H)
    expect(msUntil(undefined, now)).toBeUndefined()
  })
  test('the access level is named, and an unknown one by its number', () => {
    expect(tierOf({ accessTier: 99 } as Agent)).toBe('Tier 99')
    expect(tierOf({ accessTier: 0 } as Agent)).not.toBe('Tier 0')
  })
  test('every connection state the page can sort by has a word, a dot and a colour', () => {
    for (const h of HEALTH_ORDER) {
      expect(HEALTH[h].label).toBeTruthy()
      expect(HEALTH[h].dot).toBeTruthy()
      expect(HEALTH[h].text).toMatch(/^text-/)
    }
  })
})
