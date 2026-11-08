import type { OverrideMeta } from './types'

type Raw = { source?: string; overrides?: Record<string, unknown>; overrideMeta?: Record<string, OverrideMeta> }

const value = (raw: Raw, field: string) => (raw as Record<string, unknown>)[field]

/** Where a field's value comes from, for the mark beside it: an agent reports it, you set it, or nobody tracks it (a record you declared). */
export type Origin = 'detected' | 'you' | undefined

const present = (v: unknown) => v !== undefined && v !== null && v !== '' && !(Array.isArray(v) && v.length === 0)

/** Whether an agent reported a value for this field (the stored base value, before any of your overrides). Only records from discovery have one. */
export const isDetected = (raw: Raw | undefined, field: string): boolean => raw?.source === 'discovered' && present(value(raw, field))

/** Whether you have overridden this field on a record from discovery. */
export const isOverridden = (raw: Raw | undefined, field: string): boolean => raw?.source === 'discovered' && !!raw.overrides && field in raw.overrides

/** The mark for a field: yours when you overrode it, detected when an agent reports it. A record you declared has no marks: every field is yours. */
export function originOf(raw: Raw | undefined, field: string): Origin {
  if (isOverridden(raw, field)) return 'you'
  if (isDetected(raw, field)) return 'detected'
  return undefined
}

/**
 * Whether the edit form must keep a field locked. A field is locked while an agent reports it and you have not chosen to override it; a
 * value discovery only guessed, and the fields that are yours by nature (`open`), are never locked.
 */
export function isLocked(raw: Raw | undefined, field: string, o: { opened: ReadonlySet<string>; guess?: boolean; open?: readonly string[] }): boolean {
  if (o.open?.includes(field) || o.guess) return false
  return isDetected(raw, field) && !o.opened.has(field)
}

/** The fields that start open in the edit form: those you already overrode, since changing them again is not a new decision. */
export const initiallyOpen = (raw: Raw | undefined): Set<string> => new Set(raw?.source === 'discovered' ? Object.keys(raw.overrides ?? {}) : [])
