import { Boxes, Cable, Folders, History, LayoutDashboard, Layers, MapPin, Network, Package, Radio, Server, Waypoints, type LucideIcon } from 'lucide-react'

/** The icon of each page: the sidebar draws it beside the name, and the page's empty state draws it above its title, so the two always agree. */
export const PAGE_ICON = {
  topology: Network,
  clusters: Boxes,
  applications: Layers,
  sites: MapPin,
  nodes: Server,
  namespaces: Folders,
  services: Package,
  devices: Radio,
  pipeline: Waypoints,
  fusion: LayoutDashboard,
  agents: Cable,
  history: History,
} as const satisfies Record<string, LucideIcon>
