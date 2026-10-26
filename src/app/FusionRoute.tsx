import FusionPage from '@/features/fusion/FusionPage'
import { useDataPath } from '@/features/pipeline/lib/useDataPath'
import { useServer } from '@/store/server'

/** The FUSION section, given the data path the Pipeline page counts: FUSION draws who sends in beside its own parts, and the features do not
 *  import each other upwards, so the shell hands it over. */
export default function FusionRoute() {
  const admin = useServer((s) => s.isAdmin)()
  const hops = useDataPath(admin)
  return <FusionPage hops={hops} />
}
