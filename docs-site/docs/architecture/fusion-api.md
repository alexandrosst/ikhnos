---
id: fusion-api
title: Reading FUSION (the shared API)
description: One token-protected API over the metrics, logs and traces FUSION saved - each signal on its own, or joined around a trace into one object - and exactly what a token can and cannot see.
---

# Reading FUSION

[FUSION](./regional-operators.md#fusion-where-a-regional-operator-saves-what-it-receives) saves what the central operator receives: metrics in Prometheus, logs in Loki, traces in Tempo, three stores that authenticate nothing and are never exposed. The shared API is the one way another system reads them back: a decision engine that wants the spans and logs behind a slow request, a dashboard, a script. It lives in the ikhnos server, under `/api/v1/fusion`, so there is nothing more to install and one credential model for everything.

It does two jobs. It lets a caller read each signal **on its own**, with filters that are the same across the three stores (service, namespace, pod, cluster, a time range). And it **fuses** them around a trace: one call returns the trace's spans, each carrying the log lines written under its span id, and each resource (a service in a pod in a namespace) carrying the metric series saved for it around the trace's time.

<Figure name="TelemetryFlow" caption="FUSION is part of the server's own release and switched off until enabled. Telemetry goes in through one mTLS door; the shared API reads it back from the three stores." />

## Who can read

There are two kinds of caller, and nothing else is accepted.

**A FUSION access token** (`cnf_…`) is for another system. An administrator of the server's main organisation makes one on the **FUSION** page, **Access** tab (**New API token**), gives it a name, picks which signals it may read, optionally limits it to some namespaces, some cluster ids and some ikhnos applications ([a token limited to applications](#a-token-limited-to-applications)), and chooses how long it lasts (the page offers 30, 90, 180 or 365 days, 90 by default; through the API `expiresInDays` takes 1 to 365, or 0 for the default). The secret is shown once; only its SHA-256 is stored, as with every other secret here. A token is read-only, it can be revoked at once from the same tab, and it opens nothing but this API: presented to any other part of the server, it is refused.

**A signed-in administrator** of that organisation (a browser session or a personal access token) can read everything, which is what lets the UI explore the data without minting a token for itself. A viewer or an editor cannot: the stored telemetry is more than the topology they can already see.

Making and revoking a token are audited, with the name and the scope and never the secret. Reads are not audited one by one (a dashboard would write thousands of rows); each token records when it was last used, and each caller is rate limited (600 reads a minute, bursts of 60; a request that asks the stores several times, such as a batch of traces, an overview or the list of applications, counts at least once for each store it asks, and never more than half the burst). A token belongs to the organisation, not to the administrator who made it: it keeps working if that person leaves, and any administrator of the organisation can list and revoke it. A token whose stored scope cannot be read back is refused rather than treated as unlimited. Expired tokens are removed from the list when the next token is made in the same organisation (and only in it), with one audit row for the removal, `fusion-tokens-purged`, under the server's own name (`system`), with the count and the person whose new token set it off (`2 expired tokens removed while alice made a token`); a removal that finds nothing writes no row.

```bash
curl -H "Authorization: Bearer $TOKEN" https://ikhnos.example/api/v1/fusion/status
```

A server that kept the chart's default certificate is self-signed, and curl refuses it (`SSL certificate problem: self-signed certificate`). To try the token, add `-k`, which skips the check (the dialog that shows a new token puts it in the command when the address is an IP or `localhost`, where a certificate from a public authority is not issued). To keep the check, save the server's certificate once (`openssl s_client -connect ikhnos.example:8080 </dev/null | openssl x509 > ikhnos.crt`) and add `--cacert ikhnos.crt`. The default certificate names the address in `agent.publicAddress` and in `admin.publicURL` as well as `localhost`, and any other address you reach the server by goes in `admin.tls.selfSignedHosts`; if the check fails with a name mismatch, see [the certificate does not name the address](../troubleshooting/common-errors.md#a-tool-says-certificate-verify-failed-or-a-name-mismatch-against-the-servers-own-certificate). A certificate from your own CA, or one a load balancer serves, needs neither.

A browser on another origin cannot call it: the `Authorization` header is not among the headers the server's CORS policy allows, the same as for personal access tokens. It is a server-to-server API.

**When the person who made a token is no longer an administrator.** Nothing is revoked on its own, since a token belongs to the organisation, but nobody is left to wonder about a token whose maker has gone. In the list (`GET .../fusion/tokens`) each token carries `creator`, worked out when the list is read:

| `creator` | Means |
|---|---|
| `admin` | Its maker is an administrator or an owner of the organisation now, with an account that is not disabled. |
| `gone` | Its maker is no longer one: they left or were removed, were given a role below administrator, or their account was disabled or deleted. The **Access** tab shows it with a **creator no longer an admin** mark (its tooltip names the maker). Made an administrator again, the mark goes. |
| `unknown` | The token was made before the server recorded its maker's account id (before schema 20). Its `createdBy` username is not taken as proof of who it was, because a deleted account frees its name for someone else, so nothing is claimed either way. |

A token also carries `createdById`, its maker's account id (absent on an `unknown` one). When an owner removes an administrator from **Members & access**, or gives them a role below administrator, and that person made tokens, the confirmation says how many and names them, with a box **Also revoke the N FUSION tokens they made**. It also says how many of the organisation's tokens are from before makers were recorded (they might be that person's; check them on the **Access** tab), and asks for that reason alone when there are such tokens. If the token list cannot be read for a reason other than the expected ones (another organisation, or FUSION not available), it still asks, says the tokens could not be checked, and offers no box: the change is never made without saying so. It starts unticked, since the tokens belong to the organisation. Ticked, the tokens are revoked once the member's change has been made, so a change the server refuses revokes nothing. The revoke is its own call, guarded exactly as revoking one token is (an administrator of the main organisation), and each token it ends gets the same `fusion-token-revoked` audit row as a manual revoke:

```bash
# $PAT is an owner's or an administrator's personal access token; the id is the member's, as GET .../members gives it.
curl -X POST -H "Authorization: Bearer $PAT" -H "Content-Type: application/json" \
  -d '{"createdById": "u-3f9a1c2b7e"}' https://ikhnos.example/api/v1/orgs/default/fusion/tokens/revoke
# {"revoked":["fk-…","fk-…"]}
```

It ends only that organisation's tokens made by that account, and a person who made none gets `{"revoked": []}`. Each token is revoked on its own, so a failure part of the way leaves the earlier ones revoked: the error answer then carries them too (`{"error": "1 of the tokens were revoked, then revoking the next one failed: …", "revoked": ["fk-…"]}`, an empty list if the first one failed), and the members page says some tokens may not have been revoked. A token marked `unknown` names no one and is never matched: revoke it on its own.

## What a token can see

A token's scope has three parts: the **signals** it may read (metrics, logs, traces), the **namespaces** whose telemetry it may see, and the **clusters** whose telemetry it may see. Empty namespaces or clusters means no limit on that part. A token may also be limited to some **ikhnos applications**, which narrows it further ([below](#a-token-limited-to-applications)).

The limit is enforced twice. Every query a limited token causes is built by the server from structured filters, never from text the caller wrote, with the scope's matchers added to it: a `k8s_namespace_name` matcher in the PromQL selector, in the LogQL stream selector and in the TraceQL condition, and a `ikhnos_cluster_id` one beside it (in Loki `ikhnos_cluster_id` and `ikhnos_org_id` are stream labels, so they work in the selector and after a pipe). Then each answer is checked again against the scope before it leaves, so a store that ignored a matcher still cannot leak a line. A log line is placed by its stream's labels, which the agent stamped and Loki's own filter matched, not by the attributes of its record, which are its sender's: a line whose structured metadata names another namespace or cluster than its stream is not given to a limited token. Telemetry that carries no namespace (node metrics, say) is invisible to a token that limits namespaces: the limit fails closed.

The signals limit what a token reads from the three stores (metrics, logs, traces). What ikhnos itself knows (the applications, their services, the topology between them, the recorded changes, and where a service runs) is visible to any token within its namespaces and clusters, whatever signals it may read: it is ikhnos's own model of the landscape, not telemetry FUSION collected, so a token that may read only traces still sees the applications and the topology of its namespaces. It is narrowed by the namespaces and clusters of the token in the same way as telemetry: a token sees only the services, links and events of what it may see, never the number of members it may not, and no address outside the clusters.

Two consequences follow, and both are deliberate:

- **A limited token's own PromQL is read within its limits** (except a token limited to applications, which may send none: [below](#a-token-limited-to-applications)). PromQL a token writes (`metrics/query`, `metrics/query_range`, and `promql` in a fused read) is parsed with Prometheus' own parser, and the token's namespaces and clusters are added as matchers to every selector in it, in a range, a subquery or under a `label_replace`: every series the query reads is one the token may see, whatever it then does with them, so what comes out (a `sum` without the namespace) is the token's to have. A query the parser cannot read is a `400` without reaching Prometheus, and `info()`, which reads series of its own past the query's selectors, is refused, as are Prometheus's experimental functions (whose reads may change from one release to the next). LogQL and TraceQL written by hand (`logs?query=`, `traces?q=`) and the stores' own APIs below still need a token with no namespace and no cluster limit (or an administrator). Everything else works for every token. `access.promql` in `/status` says whether you may send PromQL.
- **A limited token sees spans, not whole traces.** A trace that crosses namespaces comes back with only the spans of the namespaces the token may see; a span whose parent is hidden is shown as a root, and the hidden parent's id is not given. A trace with no visible span reads as not found, which is also what a trace that does not exist reads as, with the same message. In a search result the trace's root service and name are left blank for a limited token (the root may be in a namespace it cannot see), and its start time and duration are those of the spans that matched, not of the whole trace; it gets the services of the spans that matched. Those spans are checked against the token's scope once more, as a whole trace's are: a matched span in a namespace or cluster it may not see is neither counted nor named, and a hit with none left is not listed.

### A token limited to applications

A token can name up to 20 ikhnos applications by id (**Applications** in the **New API token** dialog; `"applications": ["app-…"]` in `POST .../fusion/tokens`). Each must be an application of the organisation when the token is made and give it at least one service: one with a namespace and a cluster ikhnos knows, within the token's own limits. Together they may give it at most 500 services in at most 100 places (a namespace of a cluster). Otherwise it is a `400`, and while the applications cannot be read the token cannot be made (`503`).

**Made with the services of the moment, as approved.** A token is made with exactly the services its applications have when it is made, however each came to be in them (a person put it there, or ikhnos placed it by its labels: `ikhnos.io/application`, a Helm release, an Argo CD application, `app.kubernetes.io/part-of`). Before it is made the dialog lists them, application by application, each by name and kind in its namespace and cluster, and the administrator approves that list: it is sent as `"members": ["<service id>", …]` (ids as `/applications` lists them), the token is made with those alone, and it is refused with `409` if one of them is no longer there. Through the API, leaving `members` out takes every service the applications give the token now. The token records them (`applicationMembers`: `[{"application": "app-1a2b", "id": "…", "name": "cart", "kind": "Deployment", "namespace": "shop", "cluster": "cl-1"}]`) and never sees more: **to widen a token, make a new one.** The audit row names the applications and how many services where (`…; clusters all; applications Shop (app-1a2b) with 2 services in shop/cl-1; expires …`). The list (`GET .../fusion/tokens`) gives each token's `applications`, each by `id` with its `name` now, `"gone": true` (and no name) for one that no longer exists, and the id alone when the applications could not be read for the list, and its `applicationMembers` (the **Access** tab shows them folded under a summary); a token not limited to applications has `[]` for both.

**Only ever fewer.** At every request each recorded service counts only while its application still has it, by the same id, under the same name and kind, in the same namespace and cluster. A service that joins an application afterwards, whether a person adds it or its labels do (team B installing a Helm release of the application's name in its own namespace), gives the token nothing; one that leaves (its label removed, or a person takes it out) is gone from the token; one that moves to another namespace, is renamed or changes kind is visible neither where it was nor where it is. A service's `app` labels, which ikhnos otherwise takes as other names its telemetry may carry, never count, and a workload's name is matched only under the label of its own kind (a Deployment's as `k8s_deployment_name`). So a service whose telemetry carries only its app label as `service.name` is found by its workload's name (metrics, spans), not in its logs.

**What it sees.** Its rights are those services: each in the namespace and cluster it runs in, under its name, within the token's namespaces and clusters. Within them:

| Telemetry | Is the token's when |
| --- | --- |
| A span, or a resource of a trace | its `k8s.namespace.name` and `ikhnos.cluster.id` are those of a place, and its `service.name` is one of that place's names, or the name of the workload its pod belongs to is, under the attribute of that service's kind (`k8s.deployment.name` for a Deployment, and so on). A resource that does not say its namespace or its cluster is no service's: unlike `application=`, nothing is matched by name alone. |
| A log line | its stream's `k8s_namespace_name` and `ikhnos_cluster_id` are a place's (as for every token, the stream's labels, not what the record says of itself), and its `service_name` is one of the place's names. |
| A series | its `k8s_namespace_name` and `ikhnos_cluster_id` are a place's, and its `service_name` is one of the place's names, or the workload label of a service's kind (`k8s_deployment_name`, `k8s_statefulset_name`, `k8s_daemonset_name`) holds that service's name. |
| A service, link or event of ikhnos | the service is one of those services by its id (not another service that has the same name), at its place; a neighbour and both ends of a link must be so too; an event is about such a service or link (never an application's, a node's or a cluster's own). A service names only the token's own applications among those it is in. |

So a service of the same name in another namespace is not its, nor another service of the same namespace, nor anything that names no service: a node's own metrics, the system namespaces' lines, the hosts and the collectors, the links between clusters. A fused read says `"systemLogs": "not allowed"` and leaves the node view of `metric_scope` empty, with a line in `warnings` for each; `/links` is refused, and a read of system telemetry alone is empty and says why.

Every query is built with the places in it, each place one alternative (a `(namespace and cluster and names)` filter in LogQL and TraceQL, one PromQL selector per place and name label, sent as a form when long; Prometheus's label-values call, which is GET only, is asked a batch of selectors at a time and the answers united), however many places there are: such a token is never read by the union of its names, namespaces and clusters, so even a list of names alone (the metric names, the services that have telemetry) is exact. Past 20 places the services with logs are read from the newest lines rather than asked place by place, and the answer says it is a sample. Every answer is checked against the places again before it leaves.

**When the applications change.** The applications are read at every request, from the same answer every read of them shares for at most 10 seconds (never an older one), so a service that leaves an application is gone from the token within those seconds. Nothing anyone does to the applications afterwards adds to the token. An application that is deleted gives the token nothing; when none of its services is left, the token sees nothing: every read answers `403` with the reason, `/applications` is empty, and `/status` says which application is gone or has none of its services left. If the applications cannot be read (the store is down, or the saved workspace cannot be parsed), the request is refused with `503` and `Retry-After`, never answered with the token's namespaces and clusters alone.

**What it may not do.** PromQL, LogQL and TraceQL of its own (`metrics/query`, `metrics/query_range`, `logs?query=`, `traces?q=`, `promql` in a fused read or a batch) and the backends' own APIs are refused with a `403`: a selector cannot say "this name in this namespace, that one in that", and an aggregated answer carries no labels to check. `/status` has `access.promql` and `access.rawQueries` false, `access.applications`, and the reason in `access.notes`. `application=` takes one of its own applications only; any other reads as not found. `/applications` lists its own applications, each with only the services it may see, and `/applications/{name}/topology` answers for those alone, with only the neighbours and links it may see and no addresses outside the clusters. `/services` and each service's `runsIn` are its applications' services at their own places, and `/services/{name}` of any other name is not found. Rate limits, expiry and the maker's mark are as for any token.

What the server does with the data matters too: the stores are reached over plain HTTP inside the cluster (their ClusterIP Services), and the server chart's network policy, when you turn it on, lets the server pod reach exactly those three ports and nothing else of FUSION. The server never sees telemetry *in transit* (the central operator writes straight to the stores), but it now **reads stored telemetry and returns it to token holders**. That is what the feature is; it is why only administrators of the main organisation can make tokens, and why a token's scope is narrow by choice.

## The calls

All are `GET` (but the batch read, and the backends' own POST forms), all take `from` and `to` (an RFC 3339 time, unix seconds, or an offset such as `now-15m` or `now-7d`; default the last hour, never more than 31 days, of which a trace search takes at most 7 and a log read at most 30 per query) and answer JSON. Durations and offsets accept `d` and `w` as well as `s`, `m` and `h`.

| Call | What it returns |
| --- | --- |
| `/api/v1/fusion/status` | FUSION's own state, the organisation it belongs to (`organisation`: the `id` its telemetry carries as `ikhnos_org_id`, and the `name` it was given in ikhnos), and what this caller may read (signals, namespaces, clusters, expiry, whether raw queries are allowed). |
| `/api/v1/fusion/applications` | The **ikhnos applications** (groups of services, made by a person or found by discovery), each with its services (each with an `id`, its name, namespace, cluster, kind, aliases and signals), namespaces and clusters, and which signal types FUSION has for them in the range. Pass an application's `id` or `name` as `application` to the reads below ([filtering by application](#filtering-by-ikhnos-application)). |
| `/api/v1/fusion/applications/{name}` | One ikhnos application at a glance (`name` is its id or name): its signals across all its services, recent traces, recent traces with an error span, recent error-level log lines, the metric names it reports, and its `members`, each with the signals it has where it runs. |
| `/api/v1/fusion/applications/{name}/topology` | The service-to-service traffic of one ikhnos application, now or as of an earlier time ([application topology](#application-topology)). |
| `/api/v1/fusion/services` | The **services** that have telemetry in the range (their `service.name`), which signal types each has, the ikhnos applications each is in (`applications`; empty for infrastructure that reports under a name of its own, such as an exporter), and where ikhnos has services of that name now (`runsIn`: each one's `id`, `namespace`, `cluster` and `kind`, within what you may see; a `service.name` is shared by every service called so). `application=` keeps only the services of one application. |
| `/api/v1/fusion/services/{name}` | One service at a glance: the same overview, for a single `service.name`; `namespace` and `cluster` keep the one service of that name there (one of its `runsIn`), where otherwise every service of the name is merged. |
| `/api/v1/fusion/metrics/names`, `/series`, `/range` | Metric names, series label sets, and series values over the range, filtered by `name`, `metric` (a regular expression over the name), `service`, `namespace`, `pod`, `node`, `cluster`, `application`, `category`. `/range` takes `step` and `limit`, and needs at least one of `name`, `metric`, `service`, `namespace`, `pod`, `node` or `application`; without one it answers `400`. |
| `/api/v1/fusion/links` | The latest figures of the links between clusters, for the pairs the organisation watches: loss in each direction, round trip, delay variation, path MTU, throughput and when they were measured. Each pair once, with `measuredBy`; `cluster` and `peer` narrow it ([cluster links](#cluster-links)). Needs the `metrics` signal. |
| `/api/v1/fusion/metrics/query`, `/query_range` | PromQL as written, answered in Prometheus' own shape, with its `warnings` and `infos`. A limited token's query is read with its namespaces and clusters on every selector. |
| `/api/v1/fusion/logs` | Log lines, with `service`, `namespace`, `pod`, `node`, `cluster`, `application`, `category`, `trace_id`, `span_id`, `severity` (the record's own severity, or the level Loki detected in a line that has none, such as a container's stdout: the one each line is shown with), `contains`, `order=newest\|oldest`, `limit`. With `query=` it takes LogQL as written (unrestricted callers only). Beside the lines, `clusters` names each cluster they come from once (`[{"id": "cl-7f3a", "name": "edge-paris"}]`), instead of every line carrying the name. |
| `/api/v1/fusion/traces` | A trace search: `service`, `namespace`, `pod`, `node` (traces with a span of a resource on that pod or node), `cluster`, `application`, `category`, `name`, `status=error\|ok\|unset`, `min_duration`, `max_duration` (the length of the trace; for a token limited to namespaces or clusters, of a span it may see, since a trace reaches spans it may not), `limit`. The newest traces come first. With `q=` it takes TraceQL as written (unrestricted callers only). With `fused=true` every hit also comes back as a fused trace ([reading many](#reading-many-traces)). |
| `/api/v1/fusion/traces/{id}` | One trace. With `fused=true` (or an `include` list) it is the fused object below. |
| `POST /api/v1/fusion/traces/batch` | Up to 25 fused traces in one request ([reading many](#reading-many-traces)). |
| `/api/v1/fusion/prometheus/...`, `/loki/...`, `/tempo/...` | Each store's own read API, unchanged ([the backends' own APIs](#the-backends-own-apis)). |
| `/api/v1/fusion/openapi.json`, `/api/v1/fusion/docs` | The OpenAPI description of all of the above, and a page that renders it and lets you try each call ([the API page](#the-api-page-and-the-openapi-description)). No credential needed: they describe the API, they read nothing. |

**Lists say when they are cut.** `metrics/names`, `metrics/series`, `metrics/range`, `traces` and `logs` answer with `truncated` (true when more matched than `limit` allowed), the `limit` that applied, and the `from` and `to` of the range actually read, so a caller with a full page knows to narrow the filters or the range. `/services` and `/applications` carry `warnings` that say what the answer leaves out, and the overview of an application or service carries `metricsTruncated` when its metric names were cut. To learn whether there was more, FUSION asks the store for one more than the caller's `limit`, so a store's own log shows a limit one higher than the one passed in.

## The backends' own APIs

Everything above is FUSION's own shape: one set of parameters (`from`, `to`, `service`, `namespace` ...), one error format, one access model, whichever store the data is in. For everything the three stores can do beyond that (aggregations, label and tag discovery, log volumes and patterns, TraceQL metrics, exemplars), FUSION also serves each store's **own read API, unchanged**, under its name:

| Prefix | Serves | Example |
| --- | --- | --- |
| `/api/v1/fusion/prometheus/` | Prometheus' HTTP API: `query`, `query_range`, `query_exemplars`, `series`, `labels`, `label/{name}/values`, `metadata`, `status/tsdb`, `status/buildinfo` | `/api/v1/fusion/prometheus/api/v1/query?query=sum(up)` |
| `/api/v1/fusion/loki/` | Loki's: `query`, `query_range`, `labels`, `label/{name}/values`, `series`, `index/stats`, `index/volume`, `index/volume_range`, `patterns`, `detected_labels`, `detected_fields` | `/api/v1/fusion/loki/loki/api/v1/query_range?query={service_name="web"}` |
| `/api/v1/fusion/tempo/` | Tempo's: `api/traces/{id}`, `api/v2/traces/{id}`, `api/search`, `api/search/tags`, `api/v2/search/tags`, `api/search/tag/{tag}/values` (and v2), `api/metrics/query`, `api/metrics/query_range` | `/api/v1/fusion/tempo/api/search?q={ status = error }` |

After the prefix the path, the parameters and the answer are the store's own, so a client written for the store works with only its address and a bearer token changed: a Grafana datasource of type Prometheus, Loki or Tempo with the URL `https://ikhnos.example/api/v1/fusion/prometheus` (or `/loki`, `/tempo`) and an `Authorization: Bearer <token>` header, `promtool`, `logcli`. A POST with a form (how Grafana sends queries) is accepted on the query endpoints. A bad query gets the store's own error, in its own format. Loki's `patterns` come from its pattern ingester, which is enabled, and Loki and Tempo search accept a range of at most 31 days, as Prometheus does.

What is *not* mirrored is as deliberate as what is. Only read endpoints are served: nothing that writes (remote write, OTLP and Loki push, delete, snapshot), administers (reload, quit, flush, config, ring) or tails exists at these addresses - there is no refusal to bypass, the route is simply not there (404). The same authentication, rate limit (a request is a request), 30-second timeout, 16 MiB answer cap and failure rules as the rest of the API apply, and a store's own text for a failure of the store (as opposed to a mistake in your query) is not passed on.

**Who may use it.** A parameter of this API's own (`application`, `category`, `service`, `namespace`, `pod`, `node`, `cluster`) is refused on the mirror rather than passed to the store, which would ignore it. The mirror forwards a query the caller wrote, and a query written by hand cannot be narrowed to a namespace or a cluster without understanding it, so it is for an administrator (a session or a personal access token) or a FUSION token with no namespace or cluster limit, for the signal in question (`prometheus` needs `metrics`, `loki` needs `logs`, `tempo` needs `traces`). A token that is limited keeps the structured calls above, where the limit is enforced; on the mirror it is refused with a 403 that says so. The token is what says that this caller may read what FUSION collected; the stores themselves are never exposed.

## The fused trace

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://ikhnos.example/api/v1/fusion/traces/0af7651916cd43dd8448eb211c80319c?fused=true"
```

The answer is the trace (`traceId`, `start`, `end`, `durationMs`, `services`, `spanCount`, `errorCount`, `roots`) with:

- `spans`, in start order, each with its `parentSpanId`, `depth`, `service`, `namespace`, `pod`, `status`, `attributes`, `events`, a `resource` key, `logs` (the log lines that carry this span's id) and `metrics`: the points of its resource's series that fall inside the span's own time, widened by `span_pad` either side, each series with its `points` and a `min`, `max`, `avg` and `last` computed over just those points. A series with nothing in a span's window is left off that span.
- `resources`, one per service-in-a-pod, each with its `metrics`: the series saved for the same service and namespace (what the application itself reported) and for the same pod and namespace (what was reported about the pod), over the trace's time plus a margin, each as `points` of `[unix seconds, value]` and a `min`, `max`, `avg`, `last`.
- `logs`: how many lines were found (`total`), how many landed on a span (`matched`), and `unmatched`, the lines that carry the trace id but no span id or the id of a span this read does not have.
- `joins`: in words, how each requested signal was tied to the trace. Logs are `exact` (the record carries the ids); metrics are `associated, not proven`.
- `joinedBy`: the same, with the same keys, in words a program can compare instead of reading the prose:

  | `joinedBy` | Used for | What ties it to the trace |
  | --- | --- | --- |
  | `trace_id` | `logs` | The lines carry the trace's id, and each sits on the span whose id it carries. The only exact join. |
  | `resource_window` | `contextLogs`, `metrics`, `promql` queries that name a resource variable | The same service, namespace and pod as a resource, in the trace's window (padded by `pad`). |
  | `node_window` | `systemLogs` | The nodes the trace's pods ran on, in the trace's window. |
  | `window` | `promql` queries that name no resource variable | Read once for the whole trace, over its window. |
  | `resource_or_window` | `promql` with queries of both kinds | Each query one way or the other: its results sit on the resources, or in `queries`. |
  | `topology` | `topology` | The service ikhnos knows by the same name, namespace and cluster, as of now. |
  | `changes_window` | `changes` | What ikhnos recorded about the trace's services, nodes and clusters, from `changes_before` before the trace to `pad` after it. |

- `context`: what the trace is part of, by id **and name**, since the telemetry carries ids alone: `org` (`{"id": "default", "name": "Edge lab"}`), `clusters` (each cluster the trace's resources ran in or its lines came from, `{"id": "cl-7f3a", "name": "edge-paris"}`, the name left out when ikhnos knows none) and `applications` (each ikhnos application any of its resources is part of). It describes the whole trace, all of its resources, as `resources` does: span filters and `application=` narrow what is joined, not the context. Each resource also carries `clusterName` beside its `cluster` id, in a fused read and in a plain one. Only what you may see is named: a token limited to some clusters is never told what the others are called, and only the applications it can see are listed. The names are read at most every ten seconds; when they cannot be read, the answer carries the ids alone and one line of `warnings` says so.
- `sources`: for each of `traces`, `logs` and `metrics` (and `contextLogs` and `systemLogs` when asked for), one of `ok`, `not requested`, `not allowed` (the token's scope does not include it), `unavailable` (the store could not be reached) or `error`; `warnings` says why. A call to a store is cut off after 10 seconds, inside the request's 30, and a store that is too slow (the 504 names it) degrades only its own part to `error`: the rest of the read comes back. **A store that is down does not fail the read**: the trace comes back and the missing part is named. Only a trace that cannot be read at all fails.

`pad` (the margin around the trace, default two minutes, at most an hour), `span_pad` (the margin around each span when its metrics are cut out, default 30 seconds, at most an hour: samples arrive every 30 to 60 seconds, so a span of a few milliseconds would often have none without it), `metric` (a regular expression over metric names, default all), `max_logs` (default 500, at most 2000), `max_series` (per resource, default 15, at most 100) and `points` (per series, default 60) shape it. A trace returns at most 5000 spans, and says when it had more (`truncated`): its error spans are kept first, then the earliest of the others, and its totals (`spanCount`, `errorCount`, `start`, `end`, `durationMs`) and with them the window every other signal is read in are those of the whole trace. The spans of one fused read carry at most 20000 metric and query points between them (overlapping spans of one pod repeat the same samples), and only the spans a `span_*` filter keeps are given any; past that a span keeps each series' summary but not its points, and `warnings` says so. A resource whose metric read failed has none to cut; the spans of the other resources still get theirs. The per-span metrics are cut from what was already read for the resource, so they cost no extra store call.

### Choosing what it carries

`fused=true` alone is `include=logs,metrics`. Giving `include` (a comma-separated list) says exactly what to join, and implies `fused=true`; `fused=false` with an `include` is refused rather than guessed at.

| `include` | What is joined | How |
| --- | --- | --- |
| `logs` | The lines that carry the trace's id, each on the span whose id it carries. | Exact. Only the trace's own services are searched, not every service in the window. |
| `context_logs` | For each resource, the lines it wrote around the trace that carry **no** trace id (the connection pool warning that came just before the failure). On `resources[].logs`, oldest first, `max_context_logs` per resource (default 50). | Associated: same service, namespace and pod, in the trace's window. |
| `system_logs` | The lines of the system namespaces (`system_namespaces`, default `kube-system`) on the nodes the trace's pods ran on, in `systemLogs`, `max_system_logs` of them (default 100). One read per node, at most five nodes; a node whose read fails is named in `warnings` and costs only its own lines. | Associated: same node, in the trace's window. A token whose namespaces exclude the system ones gets a warning, not a read. |
| `metrics` | The series of each resource, and of each span's own time. | Associated. `metric_scope` says whose: `app` (what the service reported), `pod` (what was reported about its pod) and `node` (series that name the node and no pod). Default `app,pod`. |
| `all`, `none` | Everything above; or nothing, which is the trace alone, shaped by the options below. | |

The same options narrow or shape the answer:

- `log_severity` (`error` or `error,warn`) and `log_contains` apply to every log read of the fused object, so a read for failures can leave the `info` lines out.
- `span_service`, `span_status` and `span_min_duration` return only the spans that match, for a long trace of which you want the failing part. The trace's own totals (`spanCount`, `errorCount`) stay those of the whole trace, `spansOmitted` says how many were left out, and `logs.onFilteredSpans` how many of the matched lines sat on them. What is joined to the resources follows: only a resource a kept span ran on gets its metrics, context lines, queries, topology and changes (the trace's own lines are still read for all, so that those on left-out spans can be counted).
- The search filters of a fused `/traces` (`service`, `namespace`, `pod`, `node`, `cluster`, `category`, `name`, `status`, the durations) choose which traces are returned; what is joined to each is shaped by `application`, the `span_*` filters, `metric`, `metric_scope` and the `log_*` options. To have the joins of `service=checkout` too, add `span_service=checkout`.
- `omit=attributes,events` leaves the span and resource attributes, or the span events, out; most of a large trace's bytes are these.
- `shape=compact` leaves `service`, `namespace`, `pod`, `node` and `cluster` out of each span: its `resource` key names the entry of `resources` that gives them, once for all of its spans. The default, `shape=full`, gives each span its own copy, as before. Any other value is a `400`. The plain trace in this shape is `include=none&shape=compact`.
- `pad`, `span_pad`, `metric`, `max_logs`, `max_series` and `points` are as below.

### Your own PromQL, topology and changes in the fused object

These are added to the fused read (`GET /traces/{id}?...`, `GET /traces?fused=true`, `POST /traces/batch`) next to logs and metrics. Each reports itself in `sources`, `joins` and `warnings` like the rest.

#### Your own PromQL (`promql`)

`promql=<name>=<expression>`, repeated for up to five queries (in a batch body: an object of name to expression, or a list of `name=expression` strings). Giving `promql` implies `fused=true`; with `promql` alone, only your queries are added (no logs or metrics unless you also give `include`).

A query may name the resource it is read for: `${service}`, `${namespace}`, `${pod}`, `${node}`, `${cluster}`, `${workload}` (the Deployment, StatefulSet, DaemonSet, Job or CronJob its pod belongs to, from `k8s.deployment.name` and the like on the resource; a resource that names none is skipped). Such a query is evaluated once per resource of the trace with its values filled in, and the series sit on that resource (`resources[].queries`). With `promql_spans=true` they are also cut to each span's own time (plus `span_pad`), the same way `metrics` are (`spans[].queries`). A query that names none of them is evaluated once over the trace's time and sits in `queries` on the fused object. `${trace_id}` and `${range}` (the window's length as a PromQL duration, such as `154s`) are available to both kinds, and so are `${application}` and `${application_id}` when the read is narrowed to an application (`application=`; a query that uses them without one is a `400`). A query about the whole application joins on the [info series](#from-prometheus-grafana-and-other-tools-that-read-the-stores-directly): `sum(rate(http_server_request_duration_seconds_count[${range}]) * on(service_name, k8s_namespace_name, ikhnos_cluster_id) group_left ikhnos_application_info{application_id="${application_id}"})`. With `application=`, the per-resource queries are evaluated for the application's resources only, and a query for the trace as a whole (or one naming only the resource's `${namespace}` or `${cluster}`, which would otherwise read every service there) reads the application's own series: its service names (`service_name`, a member's name and aliases), and its namespaces and clusters when every member has one, are added to every selector of the query, so `sum by (k8s_pod_name) (increase(container_cpu_time_seconds_total[1m]))` is the CPU of the application's pods and not of every pod. It is the union of the members (for an application in several namespaces a name can match in another member's namespace), and it matches the application by `service_name` only: not also by the workload's name (`k8s_deployment_name`, `k8s_statefulset_name`, `k8s_daemonset_name`) as a metrics read narrowed to the application does, because a selector cannot say "this label or that one" and the query cannot be rewritten to read both without changing what it computes (a range such as `rate(x[1m])` would have to become a subquery). Pod and container series carry `service_name` from agents with `transform/service_name` (on by default), not in data stored before. When such a query returns no series at all and some of the application's resources in the trace are part of it by their workload's name rather than their `service.name`, the answer's `warnings` say so and name them; a per-resource query that selects on `${workload}` reads those by the workload's name, and `promql_as_written=true` sends the query as written. The `${application_id}` join above is the exact alternative; `${service:re}` is the value escaped for use inside a regular expression; `$${` writes a literal `${`.

```
GET /api/v1/fusion/traces/<id>
      ?promql=errors=sum(rate(http_server_errors_total{service_name="${service}",k8s_namespace_name="${namespace}"}[1m]))
      &promql=cluster_load=avg(node_load1)
      &promql_spans=true&promql_pad=10m
```

| Parameter | Meaning |
| --- | --- |
| `promql_spans` | also cut per-resource results to each span |
| `promql_step` | seconds between points (default: chosen from `points`, at least one second) |
| `promql_pad` | window around the trace for these queries (default `pad`, at most one hour) — a `rate(...[5m])` over a 200 ms trace needs more than the default |
| `promql_services` | evaluate per-resource queries only for the resources of these services, and read a query for the whole trace (or one naming only `${namespace}` or `${cluster}`) for these services' series only |
| `max_series` | the most series per query result |

Rules worth knowing:

- A caller whose signals include `metrics` can run queries; one without gets `sources.promql = "not allowed"`. A caller limited to certain namespaces or clusters has them added to every selector of each query, as on `GET /metrics/query`, so a query reads only what it may see; one the parser cannot read has an `error` of its own, without reaching Prometheus.
- A value is substituted only if it is made of letters, digits and `_ . : / @ + - = , space` (at most 253 characters). A service or pod name with a quote, a backslash, a brace or a control character is not substituted; that resource is skipped and the warning says why. Telemetry names are data from outside, so they cannot change the shape of your query.
- A resource that lacks a value the query names (a service with no pod) is skipped, and the warning names it.
- Identical expanded expressions are evaluated once, whatever the number of resources or queries that produced them. One read evaluates at most 60 distinct expressions: the queries about the whole trace first, then the per-resource ones in order; an expansion past the limit is in the answer with `error` "not evaluated", and `warnings` names its query.
- A query that Prometheus refuses fails alone: its result carries `error`, the other queries and the trace come back, and `sources.promql` is `error`.
- Like `metrics`, the join is associated, not proven: a metric sample carries no trace id.

#### What ikhnos knows (`include=topology`, `include=changes`)

`topology` adds `resources[].topology`: the service as ikhnos knows it (matched by name, namespace and cluster, falling back to its `app` label), the services and external addresses it was seen calling (`calls`) and the services seen calling it (`calledBy`). It is the topology as of now, not as of the trace, and says so in `joins`. DNS and system traffic is left out.

An empty namespace or cluster on either side is not a wildcard. A resource that matches more than one service is ambiguous: it gets no topology, a warning, and a sentence in `joins` saying so.

`changes` adds `changes`: events ikhnos recorded about the trace's services, nodes, clusters, dependencies and applications (`targetKind` is `cluster`, `node`, `service`, `dependency`, `application` or `agent`) from `changes_before` (default 30 minutes) before the trace to `pad` after it — scalings, restarts, node and cluster events, each with its detail and cause when known. `max_changes` (default 50, at most 500) keeps the newest. Events are looked up by the ids of the trace's resources, not by name; a node's id comes from the current topology, so events of a node that has already left it are not found.

Neither is a token signal. They follow the token's namespaces and clusters: a token limited to namespaces sees only neighbours in those namespaces (and no external addresses), and only the events of services in them. `include=all` now includes both. If the server has no topology to read, `sources` says `unavailable` and the trace still comes back.

### What the join rests on

The join is only as good as the instrumentation behind it, and it is worth saying where it can come apart:

- **Logs to spans** use the OTLP log record's own `trace_id` and `span_id`, which Loki keeps as structured metadata. An application that logs without its trace context (most do until the logging bridge of its OpenTelemetry SDK is turned on) has logs, and traces, and nothing that joins them.
- **Metrics to spans** are placed by resource and by time, never by request. They use resource attributes that the FUSION chart promotes to Prometheus labels: `service.name`, `k8s.namespace.name`, `k8s.pod.name` and a few more. The authoritative list is `fusionapi.Vocabulary` in the server code; the chart side is `prometheus.promoteResourceAttributes` in the FUSION chart's `values.yaml` (and, for Loki, the `otlp_config` index list). The join is "the same service, namespace or pod around the same time", not a causal link from one request to one sample: metrics do not carry trace ids.
- **Telemetry carries a `service.name`, not an application.** An application in ikhnos (a group of services, made by a person or found by discovery) is a different thing, and telemetry does not carry it: `/applications` lists the ikhnos applications, `/services` the `service.name`s, and [filtering by application](#filtering-by-ikhnos-application) resolves one to its services when you ask.
- **Clocks.** Spans, log lines and samples are stamped by the nodes that produced them. The margin around the trace absorbs ordinary skew, not a node whose clock is minutes out.

## Filtering by ikhnos application

In ikhnos an application is a group of services: a person made it, or discovery found it. The telemetry itself only says which `service.name`, namespace and cluster it came from, so FUSION does not stamp an application on anything. Instead `application=<id or name>` is resolved **when you ask**: the server looks the application up in what ikhnos holds now, takes its services (by name, and by their `app` label when telemetry uses that as its `service.name`) and the namespaces and clusters they run in, and adds them to the query as ordinary filters. Edit an application in ikhnos and the next read follows; nothing stored is rewritten. `/api/v1/fusion/applications` lists what you can name.

Why the application is not stamped on the telemetry as it arrives, as the organisation is (`ikhnos_org_id`): it would make filtering one exact label match, with no limit on the number of places, but it would be wrong in ways that cannot be put right afterwards.

- **Membership changes, stored data does not.** An application edited today would leave last week's lines labelled with last week's members; asked about now, the store would answer about then. Resolving at read time always answers with the application as it is.
- **A service can be in several applications.** A label holds one value, so a service in two applications would need a list in a label, which none of the three stores can match exactly.
- **The gateway would need the membership.** The central gateway's configuration is rendered once, at install; stamping from ikhnos's applications would need the server to push a lookup table to it and the gateway to reload it on every edit.

So the exact join is kept where a store can do it at read time, the [info series](#from-prometheus-grafana-and-other-tools-that-read-the-stores-directly) in Prometheus, and the API resolves `application=` to each member's place for the three stores. An application in more places than one query can name one by one (twenty) is matched by the union of its names, namespaces and clusters, and the answer says so in `warnings`. Labelling the workloads themselves (a pod label the agents' `k8sattributes` copies onto the telemetry, such as `app.kubernetes.io/part-of`) is the way to give the telemetry an application of its own, when one is wanted that does not change with ikhnos.

That is also the answer to "should the application name be stamped on the telemetry?": nothing needs it to be. The name is resolved at query time, so it is always the present membership, no collector has to be reconfigured or restarted when an application is edited, and history is not frozen under an old name. Stamping it in the central operator would need a static lookup there (a configuration change and a restart per edit), and the central operator cannot see which pod belongs to which application; the right place for it, if ever needed for tools that read Loki or Tempo directly, is the source, where the agent already knows the pod.

```bash
# the applications you can filter by, and what is in each
curl -s -H "Authorization: Bearer $TOKEN" $ADDR/api/v1/fusion/applications

# the services telemetry names, and which application each is in
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/services"

# the failing traces of one application, the logs of the same application, the metric names it reports
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/traces?application=Shop&status=error&fused=true&include=logs,metrics"
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/logs?application=Shop&severity=error"
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/metrics/names?application=Shop"
```

What it does and does not do:

- It narrows **the lists, searches and metric reads** (applications, metrics, logs, trace search). It is combined with the other filters and with the token's own limits, never widened by them: a token limited to one namespace sees only the part of the application in it, and an application none of whose services it can see answers `404`, as if it did not exist.
- **On a fused read it narrows what is joined, never the trace.** A trace of the application also passes through services that are not part of it, and cutting those spans away would hide the cause of a failure, so every span stays. But with `application=` on `/traces/{id}`, on a fused `/traces` search or in a `/traces/batch` body, the trace's log lines (from the application's resources' own namespaces and clusters), each resource's metrics and context lines, the system lines of its nodes, your PromQL, the topology and the changes are read for the application's resources only, `application` on the answer names it, and `warnings` say how many resources were left out. A resource is part of the application when its `service.name`, or the name of the workload its pod belongs to, is a member's name or alias, in the member's namespace and cluster; one that does not say its namespace or cluster is taken on its name alone, and the warnings name it. Without `application=` a fused read joins every resource, as before.
- **Every resource of a fused read names its applications** (`resources[].applications`, each an `id` and `name`), narrowed or not, so a client can tell the application's part of a trace from what it passed through.
- It cannot be combined with a query you write yourself (`query`, `q`, the PromQL routes): those are sent as written (within a limited token's namespaces and clusters), so there is nowhere to add the application. Add its services to your own query (the list is in `/applications`) or use the join below.
- **Each member is matched where it runs.** Metric reads, log searches and trace searches look for each member's names (its name and aliases) in that member's own namespace and cluster, so a service called `redis` in the namespace of another member is not taken for part of the application, and a member ikhnos knows no namespace or cluster for leaves that dimension open for its own names only. This holds for an application in up to 20 namespace-and-cluster places; one in more is matched by the union of its names, namespaces and clusters, as a looser filter, and every answer narrowed to it says so in `warnings` (`application X runs in N places, more than the 20 a query can name one by one, ...`): a name of one member in another member's namespace is then read as part of the application. The lists are matched the same way: the signals of each member in `/applications` (and `members` of `/applications/{name}`) are read application by application, each where its members run, so a member has a signal only if a service of its name has it in that member's own place (past 25 applications, by name across everything you may see, and the warnings say so); `/services?application=` lists the services with telemetry in the application's places, each with `runsIn` narrowed to the application's members. Where a member comes from: a discovered service a person put into the application, and one that discovery grouped into it and a person accepted (accepting a grouping suggestion creates the application and stores no membership, because the services' own labels are the membership, so FUSION reads that label from the topology, unless a person put the service in another application), are read from the live topology, so their cluster must be connected now; a service somebody wrote into ikhnos by hand is taken from its own record (its name, namespace and cluster), since the topology only holds what agents saw. A member ikhnos has an id for but cannot tie to either is left out and counted: `unresolvedMembers` on the application in `/applications`, and the `404` for an application with no usable members says how many ('3 of its members are not in the live topology') instead of only 'has no services'. A deleted application is not listed.
- An application with no services, or more than 500 service names, is refused. So is `category=system`, or a `name` that is a system metric (`kepler_*`, `DCGM_*`, `node_*`, ...), together with `application`: system telemetry carries no service and is no application's, so the answer would always be empty.
- **Its pods' own metrics and logs are included.** The agent gives a pod's own records (CPU, memory, restarts, container logs) the name of their Deployment, StatefulSet or DaemonSet as `service.name` when they have none (`transform/service_name`, see [telemetry](./telemetry-intent.md)), so they are found like the application's own. For data stored before that, or from an agent with it turned off, metric reads also match the application's names against the workload labels (the same assumption the Grafana dashboard makes: a workload is named after its service), when you have not named a `service` yourself; logs and traces have no such fallback.
- The list of applications is cached for ten seconds, so a burst of requests (a dashboard refreshing) reads ikhnos' state once; an edit shows up within that time.
- **Your own choice is separate from your rights.** A token's namespaces and clusters (and the applications it is limited to, if any) are rights: they decide what you may see and are never widened. `application=` (like `service`, `namespace` and `cluster`) is a choice inside them. Reading one trace and the trace roots a limited token must not see follow the rights only, so choosing an application never hides part of a trace you are allowed to read; it only narrows what a fused read joins to it.
- `include=topology` also names, on each service, the applications it is in (`applications`).

### From Prometheus, Grafana and other tools that read the stores directly

Those tools do not go through this API, so while FUSION is running the server writes one series into FUSION's Prometheus every minute:

```
ikhnos_application_info{application="Shop", application_id="app-1", service_name="cart", service_id="sv-3f9a1c0d2b44", k8s_namespace_name="shop", ikhnos_cluster_id="cl-1", member="cart/shop/cl-1"} 1
```

One series per service name of each application, with the labels the telemetry's own series carry for it. Select by application with the same filters the API adds, or join:

```promql
# the services of an application, as a selector
rate(http_server_duration_seconds_count{service_name=~"cart|cart-app|web", k8s_namespace_name="shop"}[5m])

# or let Prometheus do it: an application's series from the series it labels (one application at a time)
sum by (service_name) (rate(http_server_duration_seconds_count[5m]))
  * on(service_name) group_left(application)
  max by (service_name, application) (ikhnos_application_info{application="Shop"})
```

`member` is the service, the namespace and the cluster as one value (`service/namespace/cluster`; a part ikhnos does not know is empty, not a wildcard). It exists so a selection can be exact: a join on `on(service_name, k8s_namespace_name, ikhnos_cluster_id)` keeps exactly the tuples of the members you chose, where filtering by service, namespace and cluster separately would also keep a service in a namespace it does not run in. The Applications dashboard's picker is built from it.

`service_id` is the id of the service in ikhnos (`sv-…`), the id the API gives on an application's services and the graph uses for the service entity ([system memory](./system-memory.md#entities-and-versions)); `member` is its `name/namespace/cluster` key, which the graph stores as `key`. It ties a series to one service record, where `service_name` can be shared by services in different namespaces or clusters. It adds a label, not a series: the cap of 5000 series and the report of a truncation are as before. A join or selector on the other labels works as it did; one that matches the exact label set of this series needs `service_id` added or ignored (`ignoring(service_id)`).

The series is not telemetry: the Grafana dashboards leave it out of their counts, and it appears only while FUSION runs. It carries a series for each name a member's telemetry may use (its name and its aliases) in each namespace and cluster it runs in, so a count of the series is not a count of services. A member ikhnos knows no namespace or cluster for is written without that label (the dashboards' namespace and cluster choices match those with `.*`, not `.+`). It describes the present: an application edited or deleted in ikhnos changes the series within a minute (a removed one is gone after Prometheus' five-minute lookback), and its history is not kept. It is written only while FUSION is running, and the Grafana dashboard **Applications** reads it for its variables. A service in two applications makes the join many-to-many; select one application, as the dashboard does. Loki and Tempo have no such series: use the API's `application=`, or the service list from `/applications` in a LogQL or TraceQL selector.

**The organisation by its name.** Every agent stamps its organisation's id on what it sends (`ikhnos_org_id`), not its name, which people change. The central gateway then writes FUSION's own organisation over it, whatever a sender said: only that organisation's senders hold a certificate the gateway trusts, so the label always names the organisation the data is in. The server puts the id in FUSION's settings when FUSION is turned on, and the gateway reads it when it starts, so a FUSION that was already on before this takes it up at its next start (turn it off and on, or let the next upgrade restart the gateway); until then the agents' own stamp stands, which is the same organisation's. Data stored before keeps the label it was sent with. Beside the applications and the clusters the server writes, every minute,

```
ikhnos_org_info{ikhnos_org_id="default", org_name="Edge lab"} 1
```

so a query or a panel shows the organisation by the name it has in ikhnos now: `sum by (org_name) (rate(http_server_request_duration_seconds_count[5m]) * on (ikhnos_org_id) group_left (org_name) topk by (ikhnos_org_id) (1, ikhnos_org_info))`. A rename is written within a minute; the old name's series lingers for Prometheus' five-minute lookback, which is why the join takes one series per id (`topk`), as the dashboards do for cluster names. Like the other info series it is not telemetry, and the API's metric reads leave it out unless asked for by name.

## Cluster links

`GET /api/v1/fusion/links` answers what the Sites screen shows, for a script, a dashboard or a decision engine: for each pair of clusters the organisation watches (the `links` of its settings; see [cluster links](./cluster-links.md)), the latest loss in each direction, round-trip time, delay variation, path MTU and throughput, and when the probe and the throughput test last finished. It reads the `ikhnos_link_*` metrics with a fixed set of queries, shared for 15 seconds between all callers and the Sites screen, so polling faster than that only uses your rate limit.

```sh
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/links?cluster=cl-eu-1"
```

```json
{
  "pairs": [
    {
      "a": "cl-eu-1", "b": "cl-eu-2",
      "measuredBy": ["cl-eu-1", "cl-eu-2"],
      "rttSeconds": 0.0121,
      "lossAToB": 0.02, "lossBToA": 0,
      "jitterSeconds": 0.0006,
      "mtuBytes": 1420,
      "throughputAToB": 920000000, "throughputBToA": 870000000,
      "probedAt": "2026-10-14T09:41:07Z", "testedAt": "2026-10-13T22:10:44Z"
    }
  ],
  "at": "2026-10-14T09:41:30Z"
}
```

- **Each pair appears once.** `a` sorts before `b`, whichever way round the administrator entered the pair. `lossAToB`, `throughputAToB` and the other `...AToB` figures are from `a` to `b`, `...BToA` the other way. Both clusters of a pair measure towards each other, and the answer has already chosen between them, so count a pair once (not once under each of its clusters) and do not add the two clusters' figures together.
- **`measuredBy` says whose series the figures are**: `a`, `b` or both, in that order, and empty when FUSION holds nothing for the pair (the pair is still listed, with no figures). Where both clusters measured a figure the first cluster's is used and the second's only fills a gap; `jitterSeconds` is the larger of the two and `mtuBytes` the smaller.
- **A figure FUSION does not hold is absent, never zero**: a pair that is not probed has no loss, and one that asks for no throughput tests has no throughput. Loss is a fraction (0 to 1), times are seconds, throughput is bits per second.
- **`cluster` and `peer`** keep only the pairs a cluster is in, or the one pair of two clusters. `peer` without `cluster` is a `400`. A cluster that is in no watched pair gives an empty list, not an error.
- **What a token sees.** The route needs the `metrics` signal (without it, `403`). A token limited to clusters sees only the pairs whose **two** clusters it may read, and a pair outside that is not told apart from one that does not exist. The figures are read by the server with fixed queries and then filtered by cluster id, which is why a limited token can read them although it cannot send PromQL itself. A token limited to **namespaces** is refused (`403`, with the reason): link figures are measured between clusters and belong to no namespace, and a namespace limit hides all telemetry that carries none, as it does on the other metric reads, so an empty list would only mislead. Give such a consumer a token that limits clusters instead.
- **When FUSION cannot be read** the answer is `503` (`504` when it is slow); what the store said is not repeated.

The series behind these figures are ordinary metrics: read them over a range with `metrics/range?metric=ikhnos_link_.*&category=system` (a limited token sees only the series its clusters report).

## Application topology

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/applications/Shop/topology"
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/applications/Shop/topology?at=now-3d&window=6h"
```

`GET /api/v1/fusion/applications/{name}/topology` (`name` is the application's id or name) answers who the application's services call and who calls them: the traffic between its members, its neighbours (services outside it that talk to it), the external addresses it reaches, and the recent changes recorded about it. Each link carries its traffic, and its origin and confidence are those of the application's group.

| Parameter | Meaning |
| --- | --- |
| `at` | Read the topology as of this time instead of now ([history](#history)). |
| `window` | How far back traffic is counted. Default 24 hours, at most 7 days. |
| `noise` | Whether links classed as noise are shown. |
| `placement` | `true` adds, to each service of the application, where it runs (see [Where a service runs](#where-a-service-runs)). Off by default. |
| `format` | `json` (the default) or `nodelink`: the same services and links as a directed multigraph in the node-link form that NetworkX (`nx.node_link_graph(d, edges="links")`), D3 and `jq` read as it is. Nodes are `service:<key>` and `external:<id>`, each link has a `source` (the client), a `target` (the server) and a `key` that is the id of the dependency, so two parallel links are two links. It holds what the JSON holds for that caller, and none of the telemetry pointers or changes. |

- With `format=nodelink`, the service nodes carry the same `placement`.
- It follows the token's scope like `/applications`: a limited token sees only the part of the application, and the links and neighbours, in its namespaces and clusters. An application none of whose services it can see answers `404`.
- The answer is capped at 2000 links, 200 neighbours, 50 external addresses and 100 changes. When something was cut, the response says so.

### Where a service runs

With `placement=true`, each service the topology knows gets a `placement`: the cluster it runs in and the nodes that hold its pods.

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/applications/Shop/topology?placement=true"
```

```json
{
  "name": "cart",
  "cluster": "cl-1",
  "placement": {
    "cluster": {"id": "cl-1", "name": "athens-edge", "tier": "edge"},
    "nodes": [
      {"id": "n-7", "name": "edge-a", "role": "worker", "zone": "z1", "pods": 2},
      {"id": "n-9", "name": "edge-b", "role": "worker", "pods": 1}
    ]
  }
}
```

- `pods` is how many of the service's pods run on that node. It is there only where the agent reads pods (access tier 2); where it does not, `pods` is left out and the node list can be shorter or empty.
- A node the topology does not know is not listed, so a service can have fewer nodes than replicas, and a service on no known node has an empty `nodes`.
- A service the topology does not know has no `placement`.
- It is the same for a past time with `at`: the cluster and nodes are those of the recording the answer is drawn from.
- It needs nothing beyond the token's scope. A token sees the placement of the services it may see, and the cluster of each is one it may see.

A script that prints the placement and finds the calls that cross clusters is in [`examples/topology`](https://github.com/alexandrosst/ikhnos/tree/main/examples/topology), with a test that runs it against a stand-in server (`python3 -m unittest discover examples/topology`).

### History

`at` needs the temporal graph (Neo4j). Without it the endpoint answers `404` with "history is not enabled". The application has to exist and be visible now.

- Membership is read at the exact `at`. The topology comes from the newest recording at or before `at`; its time is given as `snapshotAt`. A service that joined the application after that recording appears with an unknown status.
- Traffic counters are merged from the recording, and the neighbours are one hop from the application over the dependency edges that were open at `at`, each with its provenance.
- The graph does not keep labels, so historical aliases and a neighbour's `applications` are empty.

## Categories: system, kubernetes, application

Every metric series, log line and span is about one of three things, and `category=` lets you ask for one: **`system`** is the host, the collectors and the network between clusters (node and process metrics, scrape health, energy, the collectors' own metrics, the `ikhnos_link_*` measurements of [cluster links](./cluster-links.md); logs of the host and node, which carry no namespace), **`kubernetes`** is the cluster's own objects (pod, container, node-condition and `kube_*` metrics; logs of `kube-system`, `kube-public` and `kube-node-lease`), and **`application`** is everything else: what workloads report about themselves. Give one or several, separated by commas (`category=system,kubernetes`); all three, or none, means no filter. An unknown value is a `400`.

```sh
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/metrics/names?category=application"
curl -s -H "Authorization: Bearer $TOKEN" "$ADDR/api/v1/fusion/logs?category=kubernetes&severity=error"
```

- **A category says what a signal is about, not who owns it.** A pod's CPU is `kubernetes` whichever application the pod belongs to; `application=` answers "whose", `category=` answers "what". They combine.
- **The API works it out when you ask, and the data is also stamped.** Metrics are sorted by the family of their name (`system_*`, `process_*`, `node_*`, `kepler_*`, `dcgm_*` and upper-case `DCGM_*`, `otelcol_*`, `ikhnos_link_*`, `scrape_*`, `up`, `target_info` are system; `k8s_*`, `container_*`, `kube_*` are kubernetes; the rest are application). Logs are sorted by namespace (none is system; the three namespaces above are kubernetes). The answer says which category each result was put in: `category` on every series and log line, and `categories` (a name to category map) beside the names of `/metrics/names`. Because the API decides it from the name or the namespace, a rule changed in a release applies to old data too. **Data that arrives after the central operator is upgraded also carries the category as a label of its own**, `ikhnos_category` in Prometheus and Loki (a pipeline filter in Loki: `| ikhnos_category="kubernetes"`) and `resource.ikhnos.category` in Tempo, set at the central operator by the same rule (a chart test keeps the two the same), so Grafana, PromQL, LogQL and TraceQL can filter on it without name patterns: `sum by (k8s_namespace_name) (k8s_pod_cpu_usage{ikhnos_category="kubernetes"})`. A sender cannot choose its own: the value is always replaced. Two differences from the API on data stamped this way: a span with no namespace is `application` (the API leaves it out, since the trace store cannot match what is not there), and nothing is stamped on data stored before the upgrade, so for a past range the API's `category=` is the one to use. The application is deliberately not stamped: it is a grouping people change, so it is looked up when you ask (`application=`, or the join on the info series below).
- **Traces have two categories, not three.** A span is not a host signal: `category=kubernetes` selects spans of the system namespaces, `category=application` the others, and `category=system` is a `400` rather than an empty answer.
- It cannot be combined with a query you write yourself (`query`, `q`), for the same reason as `application=`.

## Reading many traces

```bash
# the ten newest failing traces of one service, each in full
curl -H "Authorization: Bearer $TOKEN" \
  "https://ikhnos.example/api/v1/fusion/traces?service=checkout&status=error&fused=true&limit=10&include=logs&omit=events"

# these specific traces, with the options stated once for all of them
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"ids": ["0af7651916cd43dd8448eb211c80319c", "..."], "include": ["logs", "context_logs"], "log_severity": "error,warn"}' \
  "https://ikhnos.example/api/v1/fusion/traces/batch"
```

`GET /traces?fused=true` searches and then reads every hit as a fused trace (a smaller page: 10 by default, at most 25). `POST /traces/batch` takes up to 25 `ids` and every fused option in the JSON body (lists as arrays); options given in the query string are defaults the body overrides. Both answer `{"results": [...], "summary": {requested, ok, failed}}` with the results in the order asked (the search also returns its `traces`). Each result has its own `status`: one trace that is missing, malformed or hidden from the token does not fail the others.

**Streaming.** With `stream=true` (or `Accept: application/x-ndjson`) the answer is newline-delimited JSON, flushed as it goes: for a search first a line with the hits, then one `{"type": "result", ...}` line per trace *as soon as it is read*, in the order they finish, then `{"type": "summary", ...}`. A caller can start on the first trace while the rest are still being read, and if the 30 seconds run out, the traces not yet done come back as lines with status `504` and the closing summary still arrives.

**Why it does not block anyone.** Three limits work together. One request reads at most three traces at once. Every store call a bulk read makes waits in a bulk lane of five of the server's eight store-call slots before it takes one, so any number of bulk requests together leave three slots free and a single read never queues behind them. And a bulk read of *n* traces costs *n* reads against the caller's rate limit (600 a minute), so it cannot be used to go round it. When the 30 seconds end, the traces not yet started come back with status `504` and a message, instead of the request failing as a whole, and the summary is still written.

## From Python

A token, `requests` and a few lines are all a script needs. The calls are plain `GET` requests that answer JSON, so any HTTP client works the same way.

What a caller has to do:

- Send the token as `Authorization: Bearer cnf_…`. Keep it in an environment variable, never in the script.
- Pass `from` and `to` to say which time range to read (the last hour if left out). `from` is a Python keyword, so give it through `**{"from": "now-15m"}`.
- Check the status. `401` is a bad or expired token, `403` is a call outside the token's scope, `429` is too many requests (wait and retry), and `503` means FUSION is off or still starting.
- Check `truncated` on lists, and `sources` and `warnings` on fused reads. A store that is down degrades only its own part of the answer.
- For a server that kept the default self-signed certificate, set `s.verify` to the saved certificate ([above](#who-can-read)). Do not switch verification off in code that outlives a test.

```python
import os
import requests

ADDR = os.environ["IKHNOS_ADDR"]      # for example https://ikhnos.example
TOKEN = os.environ["FUSION_TOKEN"]    # cnf_..., made on FUSION > Access

s = requests.Session()
s.headers["Authorization"] = f"Bearer {TOKEN}"
# s.verify = "ikhnos.crt"             # the server's certificate, if it is self-signed


def get(path, **params):
    r = s.get(f"{ADDR}/api/v1/fusion/{path}", params=params, timeout=40)
    r.raise_for_status()              # 401 bad token, 403 outside its scope, 429 slow down
    return r.json()


# What this token may read
access = get("status")["access"]
print(access["name"], access["signals"], access["namespaces"] or "all namespaces")

# The failing traces of one service in the last 15 minutes, each with its logs
found = get("traces", service="checkout", status="error", fused="true",
            include="logs", log_severity="error", limit=5, **{"from": "now-15m"})
for item in found["results"]:
    if item["status"] != 200:
        print(item["id"], item["status"], item.get("error"))
        continue
    trace = item["trace"]
    print(trace["traceId"], f'{trace["durationMs"]:.0f} ms', trace["errorCount"], "errors")
    for span in trace["spans"]:
        for log in span.get("logs") or []:
            print("  ", span["service"], log["line"])

# A metric over time, one point per minute
cpu = get("metrics/range", metric="cpu", service="checkout", step="1m", **{"from": "now-1h"})
for series in cpu["series"]:
    print(series["name"], series["labels"], "last", series.get("last"))

# The links between clusters
for pair in get("links")["pairs"]:
    print(pair["a"], pair["b"], pair.get("rttSeconds"), pair.get("lossAToB"))
```

The same script is in the repository as [`examples/fusion/fusion_example.py`](https://github.com/alexandrosst/ikhnos/tree/main/examples/fusion), with a test that runs it against a stand-in server (`python3 -m unittest discover examples/fusion`).

The fused read is the useful one for a decision engine: one request returns the failing traces with each log line already on the span that wrote it, so the script does not have to join three stores. For many traces at once, send their ids to `POST /traces/batch` ([reading many](#reading-many-traces)). To generate a typed client instead, point a generator at [`openapi.json`](#the-api-page-and-the-openapi-description).

## The API page and the OpenAPI description

`/api/v1/fusion/openapi.json` is an OpenAPI 3.0 description of every route, parameter and answer, and `/api/v1/fusion/docs` is a page built from it: the routes grouped, each with a form for its parameters (checkboxes for `include`, `omit` and `metric_scope`), a field for your token (kept for the browser session only; leave it empty to use your signed-in session), a Send button that calls the real API and shows the status, time and size, a *Copy as curl*, and, for `stream=true`, each line as it arrives with the time it arrived at. It also shows the shape of each object (the fused trace, a span, a resource, a log entry). The page is served with a policy that lets it run only its own script and call this server; it loads nothing from anywhere else.

The description is generated from the same table the router is built from, so a route cannot exist without being described, and a test checks that every query parameter the handlers read is described and that every route is served. Point a client generator at `openapi.json` to get a typed client.

## Limits and errors

**Parameters are checked.** A route answers `400` for a parameter it does not take (the message lists the ones it does), and for two that cannot be used together: a raw `query` or `q` is sent to the store as written, so the filters beside it would silently do nothing, and `name` overrides `metric`. A parameter given twice that is read once (`service=a&service=b`; list several values with commas where a parameter takes a list) and one sent with an empty value (`service=`, which would otherwise widen the read) are `400` too. The options that shape a fused read (`pad`, `metric`, `span_status` and so on) need `fused=true`, an `include` list or `promql` on the routes where a fused read is optional, and so does `stream=true` on `/traces`. The native `/prometheus`, `/loki` and `/tempo` routes forward whatever they are given. The API page greys out a parameter that the ones you have filled in rule out, using the same table (`x-excludes` and `x-needs-fused` in the OpenAPI description).

A store answer larger than 16 MiB is refused (narrow the range or the filters). A request takes at most 30 seconds, and a call to one store at most 10 of them. The server keeps at most 8 calls to the stores in flight at once, across all callers, and bulk reads no more than 5 of them. A fused read looks metrics up for at most 20 resources of a trace, and clamps the window it searches to 30 days (a trace longer than that covers its last 30 days); either is reported in the read's `warnings`. When a store itself fails, the caller is told which store and the status, not the store's own error text. Failures use the usual statuses:

| Status | Meaning |
| --- | --- |
| 400 | A parameter or a filter was not valid (the message says which), or a store refused the query. |
| 401 | No credential, or one that is unknown, revoked or expired. |
| 403 | The caller's scope does not include this (a signal, or a raw query). |
| 404 | No such trace or application, or none that this caller may see. |
| 422 | The answer was larger than this API returns. |
| 429 | Too many requests from this caller. |
| 503 | The store could not be reached: FUSION is off or still starting. Turn it on from the FUSION page, Settings tab. |
| 504 | A store took too long (the message names it). |

## Not in this release

There is no push: a caller polls (a bulk read can stream its results, but each request is still a request). There is no write path of any kind through this API (FUSION is filled by the central operator and nothing else). Token scopes name namespaces, clusters and ikhnos applications, not individual services or label sets. A limited token's application list is a sample, not a census: for logs it comes from the most recent log lines (Loki cannot filter a label-value call by structured metadata), and for traces from the services of its 200 most recent traces (Tempo's tag-values call does not promise to honour a scope), so a quiet service can be missing from it.
