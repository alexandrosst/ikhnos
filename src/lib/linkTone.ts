export type LinkState = 'flowing' | 'stalled' | 'broken' | 'unknown'
/** The colour of a link of each state (the line takes it from the text colour). */
export const LINK_TONE: Record<LinkState, string> = { flowing: 'text-ok', stalled: 'text-warn', broken: 'text-bad', unknown: 'text-nb-700' }
