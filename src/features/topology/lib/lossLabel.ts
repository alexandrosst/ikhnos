/** What a line that loses connection attempts says on itself ("3.2% failed"); nothing while the loss is within the usual. */
export function lossLabel(lossPct: number | undefined, band: 'ok' | 'warn' | 'hot'): string | undefined {
  if (lossPct === undefined || band === 'ok') return undefined
  return `${lossPct < 10 ? lossPct.toFixed(1) : Math.round(lossPct)}% failed`
}
