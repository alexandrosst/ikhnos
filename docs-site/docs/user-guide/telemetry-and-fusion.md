---
id: telemetry-and-fusion
title: Telemetry and FUSION
description: Turn on FUSION, choose what a cluster reports, add a regional operator if you need one, and read it all in Grafana.
---

# Telemetry and FUSION

Telemetry is optional, and it is your [observability intent](../learn/observability-intent.md) at work: nothing is collected until you choose it. There are four steps, and you can stop after any of them.

## 1. Turn on FUSION

FUSION keeps metrics, logs and traces, and shows them in Grafana. Nothing runs while it is off.

<Video src="fusion" label="Turning FUSION on, then its overview, data, access and settings pages" caption="FUSION starts its stores one by one. When every part is healthy, the overview says so." />

Open **FUSION** in the sidebar and choose **Turn on FUSION**. When it says *Nothing needs your attention*, it is ready. The other tabs show what has arrived (**Data**), how other tools read it (**Access**) and how long each kind is kept (**Settings**).

## 2. Choose what a cluster reports

<Video src="telemetry" label="The Set up telemetry wizard, from choosing signals to running the commands" caption="Pick a starting point, check each signal, choose where it goes, then run the commands in that cluster." />

On **Pipeline**, choose **Set up telemetry**. Pick a starting point, adjust the signals, choose where to send them (FUSION is the default) and generate the commands. They are shown once. Run them in that cluster, and Pipeline then shows, step by step, whether data is arriving.

## 3. Add a regional operator, if you need one

A regional operator gathers what several clusters send and passes it on. It helps when many small sites should reach one place through one address.

<Video src="new-operator" label="Creating a regional operator from the Pipeline page" caption="Name it, choose where it sends and how clusters reach it, then run the generated commands." />

Choose **New operator** on Pipeline and follow the steps. The operator appears on Pipeline and turns healthy when it first reports.

## 4. Read it in Grafana

<Video src="grafana" label="Open Grafana from FUSION, then three of its dashboards" caption="The server signs you in, so there is no address and no login. The dashboards here run on sample data." />

On the FUSION page, choose **Open Grafana**. The dashboards cover clusters and nodes, workloads and delivery health.

## Go deeper

How a signal, a scope and a destination fit together is in [Telemetry internals](../architecture/telemetry-intent.md). Operators are covered in [Regional operators](../architecture/regional-operators.md), and FUSION and Grafana in [FUSION and Grafana](../architecture/fusion-grafana.md).
