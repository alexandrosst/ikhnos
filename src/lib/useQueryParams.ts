import { useCallback } from 'react'
import { useSearchParams } from 'react-router-dom'

/** What to do to the address's parameters: a string sets one, `null`, `undefined` or `false` removes it, and a key not named is left alone. */
export type ParamChanges = Record<string, string | null | undefined | false>

/** The same parameters with `changes` applied; the original is not touched. */
export function withParams(current: URLSearchParams, changes: ParamChanges): URLSearchParams {
  const next = new URLSearchParams(current)
  for (const [k, v] of Object.entries(changes)) {
    if (v === null || v === undefined || v === false) next.delete(k)
    else next.set(k, v)
  }
  return next
}

/**
 * `useSearchParams` for state that lives in the address (a filter, a tab, a link that opens something), with the one way every page
 * changes it: several parameters at once, as one navigation that replaces the history entry rather than adding one (a filter is not
 * somewhere to go back to). Pass a function when the change depends on what is there now.
 */
export function useQueryParams(): [URLSearchParams, (changes: ParamChanges | ((current: URLSearchParams) => ParamChanges)) => void] {
  const [sp, setSp] = useSearchParams()
  const update = useCallback(
    (changes: ParamChanges | ((current: URLSearchParams) => ParamChanges)) =>
      setSp((p) => withParams(p, typeof changes === 'function' ? changes(p) : changes), { replace: true }),
    [setSp],
  )
  return [sp, update]
}

/** One parameter: its value (null when absent) and a setter that removes it for null or an empty string. */
export function useQueryParam(key: string): [string | null, (value: string | null) => void] {
  const [sp, update] = useQueryParams()
  const set = useCallback((value: string | null) => update({ [key]: value || null }), [update, key])
  return [sp.get(key), set]
}
