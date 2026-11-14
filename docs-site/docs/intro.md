---
id: intro
slug: /
title: Ikhnos documentation
description: Documentation for Ikhnos, a read-only, agent-observed model of your clusters across the cloud-to-far-edge continuum, across clusters, links, health, trust and telemetry.
---

# Ikhnos documentation

**See everything, touch nothing.** Full-stack observability for your whole landscape, from cloud to far edge.

<Shot src="readme/topology-loop" ext="gif" alt="The sites of a landscape in Berlin, Paris and Athens on a map, with the traffic between them moving along the links" caption="Berlin, Paris and Athens on the sample landscape. The dashes move with the traffic between sites." />

Ikhnos does this the same way for every cluster:

- **One command per cluster.** Run it, approve the cluster, and it appears. No kubeconfig is handed over, and no inbound port is opened.
- **Read-only by design.** A small agent looks. It cannot change anything.
- **You decide how far it looks.** That is your [observability intent](learn/observability-intent.md): per cluster, from "registered only" up to services, and it can only be narrowed, never widened. It covers telemetry too, so metrics, logs and traces are opt-in, one signal at a time.

## Start where you are

<Cards>
  <Card to="/docs/learn/observability-intent" kicker="The one idea" title="Observability intent">How far each part may look, in one page. It can only be narrowed, never widened.</Card>
  <Card to="/docs/learn/what-is-ikhnos" kicker="Never heard of it" title="What is Ikhnos?">The problem it solves, what it does and does not do, and who it is for. Five minutes.</Card>
  <Card to="/docs/journey/" kicker="Start here to use it" title="Your first hour">Six steps from nothing to a connected cluster and your first look around. Written for someone who has never seen it.</Card>
  <Card to="/docs/getting-started/quickstart" kicker="I know what I am doing" title="Quickstart">One recipe that works on any cluster. Copy, paste, done.</Card>
  <Card to="/docs/user-guide/using-the-ui" kicker="It is running" title="Using the interface">Every page of the UI, what it shows and what you can do there.</Card>
  <Card to="/docs/inside/" kicker="I want to understand or contribute" title="Inside Ikhnos">How it is built: layers, the agent protocol, the model, trust and telemetry.</Card>
  <Card to="/docs/reference/" kicker="I need a value" title="Reference">Helm values, command-line flags and the permissions table.</Card>
</Cards>

## The idea in one paragraph

The **server** is one program with one database. **Agents** are one read-only pod per cluster, and each starts its own connection, so no cluster opens an inbound port. What an agent may see is a chain of three limits that the server can only narrow. Everything else, such as telemetry, is optional.

For the longer version with figures, read [How it works in ten minutes](learn/how-it-works.md).
