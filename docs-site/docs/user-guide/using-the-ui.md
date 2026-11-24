---
id: using-the-ui
title: Using the interface
description: "A tour of every page of the interface once your server is running: the topology views, Agents, Pipeline, FUSION, History and Team."
---

# Using the interface

This assumes a server is running and a cluster is connected. If not, start with [Your first hour](../journey/index.md). It is a tour, not a reference. Each section links to a deeper page where there is one.

<Video src="tour" label="A pointer moves through the topology, infrastructure and map views, then the clusters and services tables, agents, history and search" caption="A minute in the interface, on the built-in sample landscape (Berlin, Athens and Paris)." />

## Topology

<Video src="topology" label="The topology: clusters in Berlin, Athens and Paris, a service's details, the machines, the map with its traffic, and the filter" caption="The same landscape as a graph, as machines and on a map. Click anything to see its details." />

Everything you see is one model, kept live by your agents. Edit something in one place and it changes everywhere. There are four views:

- **Application** shows your services grouped by the cluster they run in, with their dependencies as lines. Devices sit below the clusters they connect to.
- **Infrastructure** shows the machines underneath: virtual machines, bare metal and edge devices.
- **Map** shows where your sites are. Moving dashes are traffic between sites, faster when it is busier.
- **Telemetry** shows what each cluster reports.

Click a service, node or cluster to focus it. Hover a line to see its protocol, throughput and latency. The **Filter** menu narrows the picture to some clusters or applications. **Options → Show → Platform** adds the telemetry path to the graph.

## Agents

**Manage → Agents** is where every cluster starts, and where you look after the ones you have. **Connect a cluster** opens the wizard.

Expand a row to open the agent's panel. It shows what the agent may see and what it sends right now: its [observability intent](../learn/observability-intent.md). This is where an administrator narrows what an agent shares, by pausing a collector or excluding namespaces. Widening needs a `helm upgrade` on the cluster, and the panel shows the exact command.

The **Suggestions** tab holds what the server noticed but did not decide, such as an application grouping or a cluster worth connecting. Accept, correct or dismiss each one. **Seen and gone** lists agents that have left.

## Telemetry: Pipeline and FUSION

**Pipeline** shows the whole path telemetry takes, from local operators through regional operators to the central operator and FUSION, and whether each step works. What needs attention comes first, with the steps to fix it. **Set up telemetry** and **New operator** are the two main buttons.

**FUSION** is where telemetry is kept and read. Its tabs are **Overview**, **Data**, **Access** and **Settings**. **Open Grafana** is one click.

The step-by-step is in [Telemetry and FUSION](./telemetry-and-fusion.md). **System health** shows the server and each agent as they are right now.

## Lists and their panels

Clusters, Nodes, Namespaces, Services, Devices, Applications and Sites each have a table with the same filters and column picker. Click a row to open its panel.

A **lock** beside a value means an agent reports it, and a **pencil** means you set it. To change a locked value, choose **Override**. Your override is kept when the cluster is rediscovered, and **Reset** puts the detected value back. Labels, sensitivity, and the application, site and node a device belongs to are always yours.

Namespaces are read from clusters and have no form. [Namespaces and services](./namespaces-and-services.md) says how they are grouped.

## Approvals

Two things use the word approve. Approving a **new agent** is a one-time identity check: you type the code from its log. Widening what an agent shares is different. It needs an administrator and is written to the audit trail. See [Approvals in depth](./approvals-in-depth.md).

## Team and roles

Every topology belongs to one organisation, and nothing crosses between them. There are four roles:

| Role | Can |
|---|---|
| **owner** | Everything |
| **admin** | Connect and approve clusters, manage tokens, invitations and members, and widen what an agent shares |
| **editor** | Change the topology, and narrow what an agent shares |
| **viewer** | Read |

Invite colleagues from **Members & access**; invitations are one-time links that expire after 7 days. FUSION API tokens belong to the organisation and keep working when the administrator who made them leaves. Removing an administrator, or giving them a lower role, therefore asks first when they made any: it names the tokens and offers to revoke them in the same step (**Also revoke the N FUSION tokens they made**, unticked by default). The **FUSION** page, **Access** tab, marks a token whose maker is no longer an administrator ([more](../architecture/fusion-api.md#who-can-read)). A token can also be limited to some applications: the dialog lists the services each gives it, it is made with exactly those, and it sees only what they send where they ran (never a service that joins later) ([more](../architecture/fusion-api.md#a-token-limited-to-applications)). Two-factor sign-in (an authenticator app, an email code or a passkey) is turned on per person in **Settings → Account**.

## History

**History** is a timeline of what ikhnos noticed and what people did, newest first. Use the bar at the top to go back to a moment, and open any cluster, node or service to see what changed about it and why. With Neo4j enabled you can view the whole landscape as of an earlier time. Editing is off until you return to now. Without Neo4j, the same events are kept, but not the point-in-time views.

**Recent by people** is the record of who did what, for administrators. It can be filtered and exported as CSV, and it cannot be edited.

## Settings

**Settings** has three parts: **Account** (you, your password, two-step sign-in and API tokens), **Server** (the address agents dial, the agent image and outgoing email) and **Data** (export, import or clear your topology, and load the sample).

