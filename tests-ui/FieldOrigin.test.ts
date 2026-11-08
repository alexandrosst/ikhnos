import { describe, expect, test } from 'vitest'
import { initiallyOpen, isLocked, originOf } from '@/lib/fieldOrigin'

// Where a field's value comes from decides its mark in the panel and whether the edit form locks it.
const found = { source: 'discovered', version: 'v1.30', region: 'eu', tier: '', overrides: { region: 'gr' } }
const declared = { source: 'manual', version: 'v1.30' }

describe('field origin', () => {
  test('a value an agent reports is detected, one you overrode is yours, an empty one has no mark', () => {
    expect(originOf(found, 'version')).toBe('detected')
    expect(originOf(found, 'region')).toBe('you')
    expect(originOf(found, 'tier')).toBeUndefined()
    expect(originOf(found, 'ingress')).toBeUndefined()
  })

  test('a record you declared has no marks at all: every field is yours', () => {
    expect(originOf(declared, 'version')).toBeUndefined()
    expect(isLocked(declared, 'version', { opened: new Set() })).toBe(false)
  })

  test('a reported value is locked until it is opened', () => {
    expect(isLocked(found, 'version', { opened: new Set() })).toBe(true)
    expect(isLocked(found, 'version', { opened: new Set(['version']) })).toBe(false)
  })

  test('a guess, a field that is yours by nature, and a field nobody reported are never locked', () => {
    expect(isLocked(found, 'version', { opened: new Set(), guess: true })).toBe(false)
    expect(isLocked(found, 'version', { opened: new Set(), open: ['version'] })).toBe(false)
    expect(isLocked(found, 'ingress', { opened: new Set() })).toBe(false)
  })

  test('the fields you already overrode start open', () => {
    expect([...initiallyOpen(found)]).toEqual(['region'])
    expect([...initiallyOpen(declared)]).toEqual([])
    expect([...initiallyOpen(undefined)]).toEqual([])
  })
})
