---
id: components
title: The parts of Ikhnos and how they fit
description: Which parts carry telemetry (the data plane) and which decide and configure (the control plane), what the discovery agent, the controller and the IkhnosAgent resource each do in a cluster, and how they work together from one install command to a revoke.
---

# The parts of Ikhnos and how they fit

Ikhnos has two kinds of parts. Some carry telemetry from where it is produced to where it is read. Others decide what is allowed and configure the cluster. This page names each part once and says how they work together, so that the other pages can use the words without explaining them again.

## Two planes, each spread out

Ikhnos is built as a **control plane** and a **data plane**, and each of them is distributed: it has a part where telemetry is produced (in each cluster), a part in between (in a region) where there is one, and a part at the centre.

<Figure name="DistributedPlanes" caption="Rows are the planes and columns are where a part runs. The control plane has nothing of its own in a region: a regional operator is a record the server keeps and a configuration it generates, and it has no channel back unless you turn on a content-free heartbeat." />

**The control plane** is everything that decides and configures: the **server**, the **discovery agent** in each cluster, and, where you use one, the **controller** and the **`IkhnosAgent` resource**. **The data plane** is everything that moves telemetry: the **local operator** in each cluster, the **regional operator**, and **FUSION** at the centre. The two never run through each other. The server is not on the telemetry path and the agent is not on it either. It reads the collectors' counters, which is how a page can say where data stops, and that is all.

## The control plane

<Figure name="ControlPlaneParts" caption="The agent and the controller never call each other. The IkhnosAgent resource is the note between them, and the Kubernetes API is the only thing they share." />

- the **server**: where decisions, approvals and the pages live. Agents dial in to it; it never calls into a cluster;
- the **discovery agent** in each cluster: the server's one door into the cluster and the cluster's one door out. It senses (topology, flows, health), reports what it finds, and receives what the server approves, on the one stream it opened;
- the **controller** and the **`IkhnosAgent` resource**: the cluster's own installer, optional. They keep the agent, and the local operator that comes in the same install, installed and up to date.

## The data plane

<Figure name="DataPlaneParts" caption="Three tiers of one kind of thing. Telemetry goes up the solid arrows and never through the server or the agent." />

- the **local operator**: the collectors that the agent's chart installs in a cluster, one per node and one for the cluster as a whole. They gather metrics, logs and traces from the cluster and from tools such as Kepler and DCGM, and send them on;
- the **regional operator**: optional. A gateway that receives the telemetry of several clusters in a region, only from clusters that present a certificate its own CA signed, and forwards it;
- **FUSION**: optional. The central operator, which is the one door in, and the stores behind it (Prometheus, Loki, Tempo) with Grafana already connected.

A local operator can also send straight to FUSION, or to an endpoint you run, without a regional operator in between. None of the three talks to the Ikhnos server about what to do; they carry data.

## What each part is

<Figure name="ComponentAnatomy" caption="The seven parts as pieces of software: how each runs, whom it talks to, and what it holds. The two kinds of part are not alike on purpose. The parts that talk to the outside hold as little as they can, and the part that may install holds no way out." />

There is one asymmetry to know about. A regional operator is something the server knows as an entity of its own: it has a record, its own certificate authority and a lifecycle you manage from the pages. A local operator has no entity of its own. It exists through its agent, one for one, and the server knows it from what that agent reports. That is coherent, because a cluster has exactly one of each, but it is why "operator" is a first-class thing for a region and an implicit one for a cluster.

## The words, and the ones we do not use

- **Agent** is the discovery agent only: the control plane's part in a cluster. It is not the collectors.
- **Operator** is the telemetry tier: local, regional and FUSION's central operator. We never call the controller an operator, even though a Kubernetes controller with its own resource is what Kubernetes people call an operator, so that the word has one meaning here.
- **Controller** is ours, the installer. It is not the OpenTelemetry Operator, which Ikhnos does not use.
- **Relay** means only the server's request path to a cluster's agent. It is not a word for carrying telemetry.

## What is in a cluster

One Helm release, `ikhnos-agent`, holds the discovery agent (one pod) and the local operator (the collectors). They are installed, upgraded and removed together, and they are separate programs with separate permissions. The agent does not touch the telemetry; it reads the collectors' counters, which is how the page can say whether data is arriving, and at which stage it stopped if it is not.

Next to it, optionally, sits the controller (its own release, `ikhnos-controller`), and one `IkhnosAgent` resource called `this-cluster`.

| Part | Is | Talks to | Holds |
| --- | --- | --- | --- |
| Discovery agent | one pod | the server (an encrypted, mutually authenticated stream), the Kubernetes API (read) | the rights of its access tier; the right to patch one object, if a controller keeps it |
| Controller | one pod | the Kubernetes API only | the right to install the agent chart, up to the ceilings it was given |
| `IkhnosAgent` | a Kubernetes object | nobody; it is read and written | what the agent should be, and how it is going |

The two programs are kept apart on purpose. The agent is the part that talks to the outside, so it holds as little as it can. The controller holds the right to install, so it never talks to the outside: it holds no credential for the server and does not know where the server is, other than what the resource says. Neither is dangerous alone.

## The resource is how the two talk

The controller and the agent never call each other. The `IkhnosAgent` is the note they leave for each other, the way a Deployment is the note a Deployment controller reads. Its `spec` says what the agent should be: the server and its CA pin, where the one-time token is, the access tier, the collectors, the scope, the telemetry. Its `status` says how that is going: pending, installing, ready, failed.

## From one command to a running agent

The Connect page prints one command. It installs the controller; the controller does the rest.

1. `helm upgrade --install` installs the controller chart. The chart carries the resource definition, the controller and its permissions, and a small ConfigMap and Secret: what to ask for, and the one-time token.
2. When it starts, the controller makes the `IkhnosAgent` from the ConfigMap, once.
3. The controller sees the resource and installs the agent chart from it. The agent and the local operator start; the agent enrols with the server using the token and is approved on its page.

The command asks for the cluster to be connected, and the cluster is. Nothing else is run by hand.

## From a revoke to an empty cluster

Revoking an agent in the server's pages closes its connection and ends its identity. The agent finds this out the moment it happens (or when it next starts), and for an agent that a controller keeps it does one more thing:

1. it marks its own `IkhnosAgent` (`status.revoked`, with the reason). The chart gave it the right to patch the status of that one object and nothing else, so it cannot change the spec, where telemetry goes included;
2. the controller sees the mark and deletes the resource;
3. the resource's finalizer uninstalls the release, and the agent's identity Secret goes with it, whatever the resource said about keeping it. A revoked identity is of no use to anyone.

So revoking in Ikhnos leaves nothing of the agent in the cluster. To connect the cluster again, create a new token and run the same command with it: the controller treats the same ConfigMap with a new token as a new request. The agent says in its first message to the server that a controller keeps it, so the page says so too: it tells you the controller removes the agent and there is nothing to run, and keeps the `helm uninstall` commands for an agent that is still in the cluster after a few minutes. An agent that is not kept by a controller behaves as before: it stops, and the page shows the commands as the way to remove it.

## What does not work this way yet

Everything above goes from the cluster outward, plus the revoke. Changing a cluster from the server's pages after the install (the access tier, the collectors, the telemetry) is still a command that a person runs: the page prints a `helm upgrade`, or a `kubectl patch` of the resource for an agent a controller keeps. The next step is for the server to hand that change to the agent over its existing stream, for the agent to write it into the resource, and for the controller to do the upgrade. That would add no new credential and no new connection; it would add a message to the agent protocol, and it needs one decision first: **what** is collected (signals, namespaces, sampling) can come from the server, but **where** it goes (the destination and its credentials) stays with the cluster, so that a server that is compromised cannot redirect a cluster's telemetry.

The controller's own reference, with its permissions and its limits, is on [Installing and removing what Ikhnos installs](lifecycle.md).
