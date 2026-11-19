---
id: observability-intent
title: Observability intent
description: One idea that covers everything in ikhnos that watches your clusters. You decide how much each part may see, and it can only be narrowed, never widened.
---

# Observability intent

**See everything, touch nothing.** ikhnos watches your clusters, and *observability intent* is how you say how far. It is one idea, and it covers every part that looks at a cluster: the discovery agent, the local operators that collect telemetry, and the regional operators that gather it.

<Figure name="ObservabilityPlanes" caption="The intent goes down from the server to the agents. Telemetry goes up to FUSION." />

<Figure name="TrustChain" caption="Each part applies its limit itself first. Where the server can check the result, it checks it again." />

## One rule

Every limit can only get narrower than what the cluster's own owner installed. Nothing in the interface can make a part see more than its install allows. Widening back up only returns to that ceiling, and it needs an administrator.

Everything is read-only. A part that watches has no way to change a cluster.

## Who watches what

| Part | What you decide | Where you decide it |
|---|---|---|
| **Discovery agent** | How much of the cluster it reads, which optional collectors run, which namespaces it leaves out | Connect a cluster, then the agent's panel |
| **Local operator** | Which signals it collects (metrics, logs, traces), for which namespaces, and where it sends them | Pipeline, Set up telemetry |
| **Regional operator** | Which clusters it accepts data from, and where it passes it on | Pipeline, New operator |

## The discovery agent

Three narrowings stack. The tightest one wins.

**Access tier** is how much of the cluster the agent reads at all.

| Tier | Name | The agent reads |
|---|---|---|
| 0 | Registered only | Nothing but that the cluster exists |
| 1 | Infrastructure | Nodes, storage classes, ingress classes |
| 2 | Services | Namespaces and workloads, and what they depend on |

Tiers 3 and 4 are reserved. The ceiling is set when the agent is installed, the approved tier is what you pick in the interface, and the effective tier is what the agent is doing right now.

**Paused collectors.** Node details, traffic between services and path measurements are three optional collectors. You can pause each one without touching the tier.

**Excluded namespaces.** Namespaces the agent leaves out, on top of whatever its install already leaves out. The system namespaces can never be excluded.

The agent applies all three itself. The server then checks them again before it stores anything, so an agent that ignored them would still produce only the narrower picture.

## Local operators and their signals

Telemetry is a separate door, so it gets its own consent. You choose signals by name, such as Resource usage, Kubernetes events or Application logs, and each one says what it reads and what permission it needs. Nothing is on until you turn it on.

Two things are always on: a memory limiter, and redaction. Permissions only ever add, one read-only rule per signal.

Telemetry goes straight from the cluster to where you point it. The server never sees it, unless you choose FUSION, the store that runs beside the server. That is why every change is a command you run yourself.

## Regional operators

A regional operator gathers what several clusters already send and passes it on. It accepts data only from clusters you named, and only over a certificate issued for that one operator. It has no access to any Kubernetes API.

## What stays true

Narrowing in the interface limits what the agent reports, not what its credentials could read. Those are fixed when it is installed, at the tier the install chose. To take access away completely, lower `access.tier` with `helm upgrade`. [Trust and security](../architecture/trust-model.md) covers the gap in full.

## Read on

How each limit is enforced is in [Observability intent, in depth](../architecture/observability-intent.md). The signals, scopes and destinations behind local operators are in [Telemetry internals](../architecture/telemetry-intent.md). To set it all up, follow [Telemetry and FUSION](../user-guide/telemetry-and-fusion.md).
