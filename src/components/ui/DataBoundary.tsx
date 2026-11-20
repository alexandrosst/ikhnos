import type { ReactNode } from 'react'
import type { Load } from '@/lib/useLoad'
import { EmptyState, ErrorBanner, SkeletonLines } from './primitives'

/**
 * The three things every server-backed area has to say besides its content: it is still being read, it could not be read, it has
 * nothing. Written once, so every area says them the same way and none forgets one (the usual omission is the error, which then
 * shows as a skeleton that never goes away).
 *
 * - failed with nothing to show: the error, alone;
 * - failed after an earlier answer (a poll): the error above the last answer, which is still the best the page has;
 * - not read yet: `loading` (a few placeholder lines unless told otherwise);
 * - read, and `isEmpty` says there is nothing: `empty`;
 * - otherwise `children`, given the data.
 */
export function DataBoundary<T>({
  load,
  children,
  loading,
  isEmpty,
  empty,
  errorClassName,
}: {
  load: Load<T>
  children: (data: T) => ReactNode
  loading?: ReactNode
  /** What counts as nothing for this data (an empty list, say). Without it, data is never empty. */
  isEmpty?: (data: T) => boolean
  /** Shown when `isEmpty` is true: a string becomes the usual dashed empty box. */
  empty?: ReactNode
  errorClassName?: string
}) {
  const { data, loaded, error } = load
  const banner = error ? <ErrorBanner className={errorClassName}>{error}</ErrorBanner> : null
  if (data === undefined) {
    if (banner) return banner
    return <>{loaded ? null : (loading ?? <SkeletonLines />)}</>
  }
  const nothing = isEmpty?.(data) ?? false
  return (
    <>
      {banner}
      {nothing ? (typeof empty === 'string' ? <EmptyState description={empty} /> : empty) : children(data)}
    </>
  )
}
