---
id: using-the-ui
title: Using the interface
description: "A tour of every page of the interface once your server is running: the topology views, Agents, Pipeline, FUSION, History and Team."
---

# Using the interface

This assumes you have a server running and at least one cluster connected (see [Connecting a cluster](../installation/connecting-a-cluster.md) if not). It's a tour, not an exhaustive reference — each section below links to a deeper page where one exists.

## Two views of one model

Everything you see is a projection of a single shared model, kept live by your connected agents:

- **Application view** — your services, grouped by the cluster they run in, with dependencies drawn as edges between them. IoT devices sit in a row per site below the clusters they connect to; anything a service calls that isn't part of your own landscape shows up as an external endpoint.
- **Infrastructure view** — the nodes underneath (virtual machines, bare metal, edge devices), grouped by cluster, with an optional overlay of which services are scheduled on which node.

The **Filter** menu narrows the picture to some clusters or applications. Under **Workloads** it starts on **Services** (Deployments and StatefulSets); tick **DaemonSets** or **Jobs** to bring the machinery around them in. A cluster box names the cluster, where it is and how many nodes it has, and shows a percentage beside the name only when the busiest resource is past 70% requested; the networks it shares and its CNI are in the Inspector.

Edit something in one place — rename a service, correct a cluster's tier — and it updates everywhere, because there's only one underlying document.

**Platform layer.** **Options → Show → Platform** (off by default; `platform=1` in the URL) adds the telemetry path to the picture: each cluster's agent and local operator, the Regional operators, the Central operator and FUSION. They are joined by dashed telemetry edges, each labelled with the age of the last data and a state glyph. Clicking a platform node opens the Inspector with its state, what it sends to, and an **Open in Pipeline** link. While the layer is on, the **Filter** menu has **Problems only**, which narrows the picture to what has a problem.

**Reading the canvas.** Lines are drawn at right angles, routed around cards, and around any cluster they do not join, rather than through them; where a box has several lines on one side each gets its own evenly spaced slot, lines that would run together are spaced into separate lanes, and when a gap between clusters is full the rest go round the outside of the drawing, and one that has to cross another hops over it. A line leaves and arrives square to the box it joins. The arrowhead is an open chevron pointing at what is called. A dotted line is a dependency declared but not seen in traffic, a dashed one is inferred from connection tracking, and a solid one was measured; small dots travel along a measured line in the direction of the call, faster the busier it is (not with reduced motion). A line between clusters that loses connection attempts is drawn amber or red and says how many ("3.2% failed") on itself; the other numbers are in the Inspector and the hover card.

- **Click** a service, node or cluster to focus it: everything else fades outward from the click, leaving the thing itself, its lines, the entities they reach and the boxes those sit in. Click empty canvas to clear it. When you open something from search, the view goes to it and its card glows briefly.
- **Hover** a line to see its protocol, throughput and latency; the line thickens and the two things it joins are ringed.
- **Zoom** runs from 20% to 150%, and panning stops a little way past the graph, so the structure is never lost off screen. Zoomed far out, boxes keep only their names and any warning; the detail returns as you zoom in.
- **Cards** show a strip of small cells for a service's replicas (green ready, amber not ready) and, with per-pod facts collected, the pod rail. Unhealthy cards carry a faint glow and an offline status dot pulses slowly.
- The **legend** is a small chip at the bottom left (your choice is remembered), and the **minimap** only appears when part of the graph is off screen. The **↺** button next to **Options** puts every box back where the layout placed it.

## Agents

**Agents** (under *Manage*) is where every new cluster starts and where you look after the ones you have. It has three tabs: **Agents**, **Suggestions** and **Seen and gone**. The **Connect a cluster** button opens the wizard (see [Connecting a cluster](../installation/connecting-a-cluster.md)); the first step of the page's checklist does the same.

The **Agents** tab lists every connected cluster's agent: whether it's currently connected, its last heartbeat, certificate expiry, and — expand a row to open its **Agent insight panel** — exactly what it's allowed to see versus what it's actually sending right now (the ceiling / approved / effective tiers from [System overview](../architecture/overview.md); together these three settings are what the architecture docs call an agent's *observability intent*), plus any self-reported problems and their fixes. This is also where an administrator narrows what an agent shares (pausing an optional collector, excluding more namespaces) or requests it be widened — widening always needs a `helm upgrade` on the cluster's own side; the page shows you the exact command.

The **Suggestions** tab is where the server surfaces things it noticed but did not decide on its own: a newly discovered application grouping suggested from the traffic it has seen, or a *suspicion* (traffic from an onboarded cluster toward an address nothing has claimed, on a port only Kubernetes itself tends to use), which usually means there is another cluster worth connecting. Accept, correct or dismiss each one; nothing is applied automatically. The older `/discovery` address leads here. **Seen and gone** lists agents that have left, so a cluster that disappeared is a record and not a mystery.

Telemetry is not set up here. An agent's details say how many signals it sends and link to its collector on the Pipeline page, where **Change what it sends…** opens the [telemetry wizard](#setting-up-telemetry), starting from what is installed now; for an agent that sends nothing yet, **Set up in Pipeline** opens the same wizard there with that agent already chosen.

## Pipeline

**Telemetry → Pipeline** (`/pipeline`) is one page for the whole path telemetry takes: local operators, regional operators, the central operator and [FUSION](#fusion). Discovery agents are not on it; they have the **Agents** page, and each local operator links to its agent. (The older `/operators` address redirects here.)

- **What needs attention** comes first and appears only when something is wrong. Each item has a **What to do** button that opens numbered steps, with the commands to run.
- Four tiles, one per hop, show a count and when that hop last sent data. Selecting a tile filters the table below it.
- One table lists every component, with its state (**Healthy**, **Needs attention**, **Not working** or **Unknown**), its certificate (**Renewing automatically** unless renewal fails), **Sends to** and **Last data**. A regional or central operator has a row menu; a local operator has none, because a click anywhere on its row (its chevron is on the left of the name, as on the Agents page) opens or closes its picture (what it collects and where it goes), with the button to change what it sends and the one that goes to its discovery agent. Under what is installed it lists the version of each tool its release deploys (the OTel Collector, and Kepler with its engine or the DCGM exporter when they are bundled). These are the versions the release was **installed with**, taken from the chart's image tags (**pinned by digest** when a digest is set); they are not read from the running pods, so a cluster upgraded outside the chart is not reflected.

**Set up telemetry** and **New operator** are the two primary buttons. How operators are created, reached and renewed is in [Regional operators](../architecture/regional-operators.md) and [Running regional operators](../architecture/regional-operators-lifecycle.md).

## System health

**System health** shows the server and each agent as they are right now. For history and comparison it points to the **Ikhnos self** dashboard in Grafana, which draws the server and every cluster's agent over time and marks when agents joined or left. That dashboard comes with FUSION.

## FUSION

**Telemetry → FUSION** (`/fusion`) is the built-in place to save what the central operator receives (see [Regional operators](../architecture/regional-operators.md#fusion-where-a-regional-operator-saves-what-it-receives)). It has four tabs, each its own address (`/fusion`, `/fusion/data`, `/fusion/access`, `/fusion/settings`):

- **Overview** shows what needs attention first, then a picture of how data goes through FUSION (the operators that send in, the central operator, the three stores and Grafana, with an arrow for each stretch that is green while data moves, grey when nothing is known, amber when held up and red when not working; an arrow into a store is green and moving only while the central operator delivered something to that store in the last five minutes, and each box says what it is doing: **Receiving** with how much (samples, log lines or spans a second, a minute or an hour), **Quiet** when nothing came and nothing failed, **Held up** when some failed, were turned away or the queue to the store is nearly full, and **Not receiving** when everything sent to it failed. The figures are the central operator's own counters, which it pushes to FUSION's metrics store every 30 seconds, read by the server every 15 seconds; a server or an operator that does not report them yet is drawn from when each store last held a sample, a log line or a trace, as before), then the stores and the central operator in a list.
- **Data** lists your applications and their services, and which signals arrived in the last 24 hours.
- **Access** is where API tokens are made, each with a scope and an expiry; the secret is shown once. It also has the API reference and a note on signing in to Grafana and Prometheus. See [The FUSION API](../architecture/fusion-api.md) and [Grafana and the web pages](../architecture/fusion-grafana.md).
- **Settings** sets retention for each store and shows whether it fits the volume at today's growth; shortening it asks **Delete older data?** first. It is also where FUSION is turned off. See [Running regional operators](../architecture/regional-operators-lifecycle.md#fusion-retention-and-volume-size-from-settings).


## Settings

**Settings** (`/settings`) has a short list on the left, and each entry is its own address:

- **Account** (`/settings#account`) is you: who you are signed in as, the theme, your password, two-step sign-in (authenticator app, email code, passkeys) and API tokens.
- **Server** (`/settings#server`) is the address agents dial and how it is exposed, where the agent image comes from, outgoing email, and the server and chart versions. Everyone signed in can read it; administrators change it.
- **Data** (`/settings#data`) exports, imports or clears your topology and loads the sample. Signed out, it is the only page.

## Setting up telemetry

**Set up telemetry** is a guided wizard, the only way in, in three steps after you pick the cluster. **What to collect** is four starting points and every signal as a switch, always in view. **Where to send** is one merged list of FUSION, the operators in this system, backends it detected, presets, or another OTLP endpoint (an entry that cannot take a signal you chose says why), with masking and the share of traces kept just below it. **Install** says in one line what is about to happen, what changes and what does not one click below, and shows the command, which you run yourself. It then waits for the first data to arrive. You can start it from the Pipeline and Agents pages, from **Define scope from selection** in the topology, and from **Change what it sends…** in a collector's details on the Pipeline page. The design is in [Telemetry intent](../architecture/telemetry-intent.md).

## Placement and Proposals

The **Placement** and **Proposals** pages are early research and are described only in the [roadmap](../project/roadmap.md).

## Lists and their panels

The Clusters list shows each cluster with a second line under its name: **Detected · 12 s ago** for one an agent reports (or why it is quiet, stale or gone), **Declared by you** for one you described by hand, and a warning when discovery has not found its nodes, services or dependencies yet. Click a row or its name to open the cluster in a side panel, read-only, grouped into Identity, Platform, Network, Location and Policy. A **lock** beside a value means an agent reports it; a **pencil** means you set it. The pencil in the row opens the panel straight in edit mode, and **Delete** is in the panel footer.

In edit mode a value an agent reports is locked: it shows as it is, with **Override** beside it. Open only the fields you mean to change; a field you override is kept when the cluster is rediscovered, and **Reset** puts the detected value back. Site, trust zone, data residency and labels are yours and are never locked, and a value discovery only guessed can be edited or confirmed on the spot. **Reset to detected values** clears every override at once. The **Discovered** column, which says how much of a cluster is known, is off by default; turn it on from **Columns**.

Every list works the same way. Nodes: the name cell says where the record comes from and lists any active conditions (such as MemoryPressure), status has its own column, and the panel groups a node into Identity, Hardware, System, Network, Workload and Labels. Only labels are always yours; every other field an agent reports is locked until you override it.

**Services** show the image and replicas under Kind, and the panel adds Runtime (image, replicas, ports, the nodes it runs on), Policy (data sensitivity) and the services it calls, which you edit there too. Sensitivity and labels are yours; everything an agent reports is locked until you override it. **Devices** are mostly declared, so most carry no marks; the application, site and node a device belongs to are always yours, and the panel lists the services it sends data to. **Applications** and **Sites** are groupings and places you declared, so nothing in them is locked: the panel shows what an application groups, and a site's place, coordinates and clusters, with a warning when its country and coordinates disagree. **Namespaces** are read from clusters and have no form; the name cell says whether a namespace was detected, declared, or only seen in its services, and a click still leads to its services.

## Namespaces and services

Every discovered namespace and the services inside it are browsable in their own right — which cluster they're in, how exposed they are, whether a service mesh covers them, and whether an agent was actually told to look at them (some namespaces are deliberately left out of an agent's scope; see [Namespaces and services](./namespaces-and-services.md)).

## Approvals

Two different things use the word "approve" and it's worth keeping them straight: approving a **newly enrolling agent** (typing the approval code from its log, confirming it's the cluster you expect — see [Agent trust model](../architecture/agent-trust-model.md)) is a one-time identity check. Widening what an agent shares is different: it needs an administrator, and it is written to the audit trail. See [Approvals in depth](./approvals-in-depth.md).

## Team and roles

Every topology belongs to one organization, and nothing crosses between organizations. A fresh server names the first one "Default"; the first time its owner opens the interface a small dialog offers to name it (**Later** skips it), and it can be renamed from **Members & access** at any time. Four roles, most to least: `owner`, `admin` (connect and approve clusters, manage tokens, invitations and members), `editor` (change the topology, apply decisions), `viewer` (read-only). Invite colleagues from **Members & access** — invitations are one-time links that expire after 7 days. Editors can narrow what an agent shares (lower its approved tier, pause a collector, exclude a namespace) but not widen it: raising a tier toward the install ceiling, lifting a pause or removing an exclusion needs an admin. Personal access tokens are created from a browser session; a bearer token cannot create another. Changing a password keeps the account's tokens; an offline password recovery (`reset-password`) revokes them.

Each person can also turn on two-factor authentication for their own account, from **Settings → Account** (the account menu at the bottom of the sidebar leads there): it adds a standard 6-digit code from an authenticator app (Google Authenticator, 1Password, Authy, or similar) to the password check at sign-in. Setup shows a secret key to enter into the app — no camera or QR code needed — and, once a code confirms it, a set of one-time recovery codes to keep somewhere safe in case the device with the app is ever lost. Turning it off again asks for the current password.

## History

The **History** page is one vertical timeline, newest first and grouped by day, of what Ikhnos noticed (a ring: filled when it needs a look) and what people did (a square), each with a line of detail and, when there is one, the likely cause. **All / Detected / By people** and a cluster picker narrow it; it shows twenty at a time and **Show earlier** adds more. A slim bar at the top goes back to a moment (the dots on it are the changes in the window), and the side column says whether the server's picture matches the clusters' own, which links were busiest and where the recordings are kept. **Recording** (administrators change it) opens a panel with how often to record, how long to keep recordings, a size limit and how long to keep the change log; how soon an agent is called stale and how long a link may go unseen before it is called quiet are folded away under it.

Administrators also get a **Recent by people** card beside the feed, with the last few things people did and a **See all** link, and a **Changes / People** switch under the title (`/history?view=people`). **People** is the record of who did what: agents approved or revoked, settings changed, people invited or removed, the shared workspace saved. It can be filtered by person, action and time window, exported as CSV, and cannot be edited.

If Neo4j is enabled (see [System overview](../architecture/overview.md)), the **History** page adds a time scrubber and an exact-moment picker: "view the landscape as of an hour ago" is one click, with a clear banner reminding you that you're looking at the past and editing is disabled until you return to now. It also adds a per-entity **Timeline** — open any cluster, node or service to see what changed about it and why, event by event (see [System memory](../architecture/system-memory.md)). The audit entries in a Timeline are listed for administrators only; viewers and editors get the events without them. Historic views show the recorded topology; live readings such as a node's requested resources, pod counts, pressure, power and traffic counters appear in the live view only. Without Neo4j the same events are tracked in SQLite; you get the audit trail either way, just not the point-in-time views or the Timeline.
