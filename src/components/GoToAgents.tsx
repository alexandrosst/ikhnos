import { Cable } from 'lucide-react'
import { Link } from 'react-router-dom'
import { buttonClass } from '@/components/ui/buttonClass'
import { ICON_SM } from '@/components/ui/primitives'

/** What an empty page offers when no cluster exists yet: the way in is the Agents page, which owns connecting a cluster. One place for the wording and the button, so every list says the same thing. */
export default function GoToAgents() {
  return <Link to="/agents" className={buttonClass('primary')} data-testid="go-to-agents"><Cable size={ICON_SM} aria-hidden /> Go to Agents</Link>
}
