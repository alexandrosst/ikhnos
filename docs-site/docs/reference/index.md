---
id: index
title: Reference
description: Where to find the authoritative, field-by-field documentation for every value and flag today.
---

# Reference

:::info[This section is still growing]
Rendered, table-form reference pages for the Helm values and CLI flags are planned here. Until then, the source files below are already the authoritative, field-by-field documentation — every value in both charts is documented inline, right next to where it's used.
:::

| Looking for... | Read |
|---|---|
| Every server chart value, what it does, and its default | [`deploy/helm/ikhnos-server/values.yaml`](https://github.com/alexandrosst/ikhnos/blob/main/deploy/helm/ikhnos-server/values.yaml) |
| Every agent chart value | [`backend/internal/chart/ikhnos-agent/values.yaml`](https://github.com/alexandrosst/ikhnos/blob/main/backend/internal/chart/ikhnos-agent/values.yaml) |
| Every `server` binary flag | `./server --help`, or [`backend/cmd/server`](https://github.com/alexandrosst/ikhnos/tree/main/backend/cmd/server) |
| Agent self-diagnosis codes (what each means, and the fix) | [`backend/internal/agent/DIAGNOSTICS.md`](https://github.com/alexandrosst/ikhnos/blob/main/backend/internal/agent/DIAGNOSTICS.md) |
