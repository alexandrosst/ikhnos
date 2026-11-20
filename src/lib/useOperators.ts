import { api, type Conn } from '@/lib/api'
import type { OperatorDestinationEntry, RegionalOperator, TelemetryIntent } from '@/lib/types'
import { useLoad } from '@/lib/useLoad'

/** How often the operator lists are read again. Health changes within a minute or two, so a slower poll would show a stale dot,
 *  and a faster one asks the server for nothing new. */
export const OPERATORS_POLL_MS = 12_000

interface Loaded<T> {
  items: T[]
  /** True once the first answer (or the first failure) has come back - until then an empty list means "not known yet". */
  loaded: boolean
  error: string
  reload: () => Promise<void>
}

const NONE: never[] = []

/** One list that stays current: read once, then every OPERATORS_POLL_MS while the tab is visible. */
function usePolledList<T>(read: (c: Conn) => Promise<T[]>, enabled: boolean, failure: string): Loaded<T> {
  const { data, loaded, error, reload } = useLoad(read, { enabled, failure, pollMs: OPERATORS_POLL_MS })
  return { items: data ?? NONE, loaded, error, reload }
}

/** The organisation's regional operators (administrators only: GET /operators is). Polled, so a health dot that changes
 *  while the page is open changes on it. */
export function useOperators(enabled = true): Omit<Loaded<RegionalOperator>, 'items'> & { operators: RegionalOperator[] } {
  const { items, ...rest } = usePolledList((c) => api.listOperators(c), enabled, 'Could not load the regional operators.')
  return { operators: items, ...rest }
}

/** What the destination picker offers, readable by editors as well: active operators and the central one, no secrets. */
export function useOperatorDestinations(enabled = true): Omit<Loaded<OperatorDestinationEntry>, 'items'> & { destinations: OperatorDestinationEntry[] } {
  const { items, ...rest } = usePolledList((c) => api.listOperatorDestinations(c), enabled, 'Could not load the destinations.')
  return { destinations: items, ...rest }
}

/** The telemetry intents of every agent (what each was asked to collect and where to send it), kept current the same way. */
export function useTelemetryIntents(enabled = true): Omit<Loaded<TelemetryIntent>, 'items'> & { intents: TelemetryIntent[] } {
  const { items, ...rest } = usePolledList((c) => api.listTelemetryIntents(c), enabled, 'Could not load the telemetry requests.')
  return { intents: items, ...rest }
}
