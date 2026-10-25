# ikhnos-controller

Optional. Keeps an Ikhnos agent installed in this cluster from an `IkhnosAgent` resource, so `kubectl apply` can be the way an agent is asked for. It installs the same agent chart the Connect a cluster page's `helm install` does, from the same choices, and never talks to the Ikhnos server. The reasoning, and what it deliberately does not do, is in [Installing and removing what Ikhnos installs](../../../docs-site/docs/architecture/lifecycle.md).

## Install, and ask for the agent, in one command

The Connect a cluster page's `kubectl (controller)` tab prints this, already filled in, with the one-time token:

```sh
helm upgrade --install ikhnos-controller oci://ghcr.io/<owner>/ikhnos-controller \
  --namespace ikhnos-system --create-namespace --reset-then-reuse-values \
  --set maxAccessTier=1 \
  --set-string bootstrap.server=ikhnos.example.com:8443 \
  --set-string bootstrap.caPin=sha256/... \
  --set-string bootstrap.token=ikh_...
```

Nothing is created first. The chart writes the Secret that holds the token (`ikhnos-enrollment`) and a small ConfigMap that describes the agent; the controller reads that ConfigMap when it starts, makes the `IkhnosAgent` called `this-cluster` from it, and installs the agent. Watch it with `kubectl -n ikhnos-system get ikhnosagents -w`.

The same command works on a cluster that already has the controller: `--reset-then-reuse-values` keeps what it was given (an `allowTelemetry` you turned on stays on) and changes what you say. The page sizes `maxAccessTier` from the access level chosen in its wizard, so the controller holds the permissions of that tier and no more. To raise it later: `helm upgrade ikhnos-controller <chart> -n ikhnos-system --reset-then-reuse-values --set maxAccessTier=2`.

What the `bootstrap.*` values are:

| Value | |
|---|---|
| `bootstrap.server`, `bootstrap.caPin`, `bootstrap.token` | Where the server is, the pin of its CA, and the one-time token. Giving a server without a pin is refused when Helm renders. |
| `bootstrap.accessTier` | The tier to ask for, when lower than `maxAccessTier` (absent: `maxAccessTier`). |
| `bootstrap.collectors.nodeProbe`, `.flowObserver`, `.measurements` | The collectors of the Connect page. |
| `bootstrap.scope.namespaces`, `.exclude`, `.selector` | The scope of the Connect page. |
| `bootstrap.purgeIdentityOnDelete` | Delete the agent's identity Secret when the resource is deleted. |
| `bootstrap.image.repository`, `.tag`, `.digest` | The agent image, when not the chart's. |

The bootstrap makes the resource **once**. The controller records what it applied on the ConfigMap, so a resource you deleted on purpose is not made again by a restart; a different bootstrap (a new `helm upgrade` with other values) changes only the fields it owns, and leaves the rest, such as `telemetry`, as they are. When a controller is running and there is no resource, its log says so ("the agent this controller was installed to ask for is not there, and is not asked for again"): the same request, the same choices and the same token, was already carried out. Run the install command with a new token to ask again, or remove the `ikhnos.io/bootstrap-applied` annotation from the ConfigMap `ikhnos-controller-bootstrap`. The resource is yours afterwards: `kubectl edit` it, keep it in git, or delete it to uninstall the agent. The token is good for one enrollment and expires; Helm keeps the values of a release in its release Secret, so the token is there too until it expires or the release is removed.

### Without the bootstrap

The controller alone, to ask for agents yourself with `kubectl`:

```sh
helm install ikhnos-controller oci://ghcr.io/<owner>/ikhnos-controller --namespace ikhnos-system --create-namespace
```

The controller watches the `IkhnosAgent` resources of its own namespace, and installs the agent into that namespace. The CRD is in `crds/`, which Helm installs once and **never upgrades**: after an upgrade that changes it, apply it yourself (`helm show crds <chart> | kubectl apply --server-side --force-conflicts -f -`, or `kubectl apply -f crds/ikhnosagents.yaml`). The controller tells you when you have to: the definition carries a revision (the `ikhnos.io/schema-revision` annotation), the controller reads the one in the cluster, and a resource whose cluster holds an older one has a `DefinitionCurrent` condition that is `False` (and the log says it once). Without it the API server drops the fields it does not know from a resource without saying so. The commands the Connect page writes for a resource start with that `helm show crds` step, so running them brings the definition up to date. This release added `status.revoked`, so apply it when you upgrade: without it an agent the server revoked says so, the API server drops the field, and the agent logs that the definition is older than the controller instead of being removed. It also adds `status.requested` and `status.relay` (the telemetry relay below), so apply it for that too. It adds `status.workloads` and a `Pods` column as well (below); an older definition drops them without an error.

**What the release runs.** For a release that is installed as the spec asks, the controller looks at the agent's Deployment and the collectors', Kepler's, DCGM's and the node probe's DaemonSets (the ones the chart labels) every minute, every 15 seconds while a pod is missing, and writes how many pods of each are ready to `status.workloads`. The `WorkloadsReady` condition says it in a sentence, and `kubectl get ikhnosagent` shows it in the `Pods` column:

```sh
kubectl -n ikhnos-system get ikhnosagent
kubectl -n ikhnos-system get ikhnosagent this-cluster -o jsonpath='{.status.conditions[?(@.type=="WorkloadsReady")].message}'
# ikhnos-telemetry-kepler: 2 of 3 pods ready
```

`Ready` stays true: it says the release is installed as the spec asks, and a pod held back on one node (a node Kepler cannot run on, a taint, no GPU) does not make that untrue. The controller only reports it; it restarts nothing.

Built from source, the chart's image names are placeholders. Name your own:

```sh
helm install ikhnos-controller ./deploy/helm/ikhnos-controller -n ikhnos-system --create-namespace \
  --set image.repository=<registry>/controller \
  --set agent.image.repository=<registry>/ikhnos \
  --set agent.linkImage.repository=<registry>/link
```

(`scripts/publish.sh <registry>` with `ONLY=controller` publishes the controller image and this chart.)

```sh
kubectl -n ikhnos-system create secret generic ikhnos-enrollment --from-literal=token=<the token from the Connect page>

kubectl -n ikhnos-system apply -f - <<'YAML'
apiVersion: ikhnos.io/v1alpha1
kind: IkhnosAgent
metadata:
  name: this-cluster
spec:
  server:
    address: ikhnos.example.com:8443      # host:port of the server's agent listener
    caPin: sha256/...                      # from the Connect page
  enrollment:
    tokenSecretRef: {name: ikhnos-enrollment}
  accessTier: 1                            # 0, 1 or 2; absent: the controller's maxAccessTier
  collectors: {nodeProbe: true}
  scope: {namespaces: [shop, payments]}
  purgeIdentityOnDelete: false
  telemetry:                               # optional; needs allowTelemetry (below)
    signals: {resourceUsage: true, kubernetesState: true}
    destination: {endpoint: otel.example.com:4317, authSecret: otlp-token}
YAML

kubectl -n ikhnos-system get ikhnosagents
```

The agent then appears on the server's page for approval, as any agent does. Deleting the resource uninstalls the agent (`purgeIdentityOnDelete: true` also deletes its identity Secret). One agent per namespace: a second resource is reported `Blocked`.

## Values

| Value | Default | |
|---|---|---|
| `image.repository`, `image.tag`, `image.digest`, `image.pullPolicy` | `ikhnos/controller`, appVersion | The controller's image. A digest wins over the tag. |
| `agent.image.repository`, `agent.image.tag` | `ikhnos/ikhnos`, the agent chart's appVersion | The agent image the controller installs unless a resource names its own (`spec.image`). |
| `agent.linkImage.repository` | `ikhnos/link` | The cluster-link image. |
| `maxAccessTier` | `2` | The highest tier a resource may ask for, and the most the controller holds permissions for (below). |
| `allowTelemetry` | `false` | Whether a resource may turn telemetry on (`spec.telemetry`); on, the controller holds the reads the telemetry collectors need (below). |
| `bootstrap.*` | none | Ask for the agent in the same command (above). |
| `timeout` | `5m` | How long one install, upgrade or uninstall (with its hooks) may take. |

## Permissions

Kubernetes lets nobody create a ClusterRole that grants what they do not hold. To install an agent at tier 2 the controller therefore holds, cluster-wide, everything tier 2 reads (pods, services, workloads, ingresses, mesh policy); at tier 1 what tier 1 reads (nodes and classes); at tier 0 the name of one namespace. `maxAccessTier` is how to hold less: the chart renders only the rules up to it, and the controller refuses a resource that asks for more. A test renders the agent chart for every choice a resource can make and holds this chart's rules to it in both directions (nothing the agent chart creates is out of reach, nothing is held that nothing asks for).

It also holds `get` on its own namespace, by name, at every ceiling: the agent chart reads the namespace's Pod Security label while Helm renders it (`lookup`), and Helm turns a refused read into a failed install. A test finds every `lookup` in the agent chart and holds the controller to being able to make each one.

It also holds, in its own namespace, Secrets (Helm keeps its release records there, and the token and the agent's identity are Secrets) and the kinds the agent chart makes; and, in the `default` namespace, the one small Role and binding the agent chart puts there.

### Telemetry

`spec.telemetry` is the Telemetry panel's draft in a resource's words: the signals, where each comes from, the scope, the destination (or one per kind of signal), the processing and the owner stamp. A resource states all of the agent's telemetry: the controller installs the chart again with only what the resource says (the install is `ResetValues`), so a `helm upgrade` of this release is put back, and the panel's `kubectl (controller)` tab patches the resource instead. Credentials are never in it: a destination names the Secret it is read from, the same Secret the panel's command creates. A regional operator (FUSION's central operator included) is a destination with `mtls`: the Secret that holds the client certificate (`tls.crt`, `tls.key`, `ca.crt`) and the name the operator's certificate is verified against, written like this and not by hand:

```yaml
spec:
  telemetry:
    signals: {resourceUsage: true}
    destination:
      endpoint: central.ikhnos-system.svc:4317
      mtls: {secretName: central-export-mtls, serverName: central.ikhnos-system.svc}
```

The certificate is the server's to issue, so the panel's **Generate commands** step writes both: the `kubectl` command that makes the Secret (shown once) and the patch that states the destination in the resource. For an agent a controller keeps that replaces the helm command, which the controller would put back. Extra processors are not on a resource yet; the panel says so and the helm command stays.

Telemetry is off by default in the chart, because its collectors read the pods, nodes, workloads and events cluster-wide, and the agent's ClusterRole for them can be created only by someone who holds what it grants. With `allowTelemetry: true` the controller holds those reads, and may create the ConfigMaps and NetworkPolicies telemetry makes; with it off, a resource that turns a signal on is refused with a message. When the panel's `kubectl (controller)` tab needs the permission it prints the command: `helm upgrade ikhnos-controller <chart> -n ikhnos-system --reset-then-reuse-values --set allowTelemetry=true`. Take `spec.telemetry` out of a resource (and let the controller upgrade) before turning `allowTelemetry` off again, since removing telemetry's objects needs the permissions too. The same test as above holds these rules to the agent chart rendered for every telemetry choice, in both directions.

## When the server revokes the agent

The controller installs the agent with `controller.resource` set to the name of the resource. The agent chart then gives the agent one extra right, to `patch` the `status` subresource of that one `IkhnosAgent` and nothing else (the API server does not let a status patch change the spec, so the agent cannot change the server it trusts, the image, the access tier or where telemetry goes), and a revoked agent uses it to write `status.revoked.reason` on its resource. The controller deletes a resource that has it (or the annotation `ikhnos.io/revoked`, which an agent from before this said on the resource itself) (it holds `delete` on `ikhnosagents` for this), and the usual removal below follows, with the agent's identity deleted too whatever `purgeIdentityOnDelete` says. To connect the cluster again run the install command again with a new token; the same command with the same token does not ask for the agent twice.

## Removing

Delete the `IkhnosAgent` first (the controller uninstalls the agent), then `helm uninstall ikhnos-controller`. A resource whose uninstall cannot finish is let go without touching the release by annotating it `ikhnos.io/skip-uninstall: "true"`. Helm leaves the CRD in place; remove it with `kubectl delete crd ikhnosagents.ikhnos.io` when no resource remains.

## The server's telemetry requests (`relay.telemetry`)

The Ikhnos server can ask a cluster to collect more or less telemetry: which signals, and how much of the traces. It is the *what* only. It never says where the data goes, and sends no credential: the destination is in your `IkhnosAgent`, and the server cannot change it. The one-command install turns this on; with `relay.telemetry=false` (the chart's default when you install the chart yourself) the cluster keeps its own settings and answers the server so.

With it on, the agents the controller installs pass a request to the controller through `status.requested` of their resource, and the controller:

1. checks it against what you gave it: a signal can be turned on only with `allowTelemetry`, cluster links need access tier 2, and your `spec.telemetry` has to be able to send somewhere (a destination) for any signal to be on. A request it cannot take is *refused*, with the reason, and not tried again;
2. lays it over your `spec.telemetry` (the signals and the share of the traces replace yours; everything else, the destination above all, stays) and upgrades the release. Your resource is not edited, so deleting the request, or the server taking it back, returns the cluster to exactly what you wrote;
3. waits up to `relay.deadline` (3 minutes) for the agent to be ready again. If it is not, it installs your spec alone again and says *reverted*, and does not try that request again. A request with a higher revision is tried afresh.

What became of it is in `status.relay` of the resource (`kubectl get ikhnosagent <name> -o jsonpath='{.status.relay}'`), and the server's Agents page shows the same. An administrator asks from *Change telemetry* on an agent's row: its last step offers *Ask the cluster* next to the helm command and the patch of the resource, for an agent that a controller keeps and that is new enough to pass a request on. Only the signals and the share of traces are sent; anything else the form changes (the destination, the scopes, the tags) is listed there as left out, and still needs the command. Turning signals *off* needs nothing from `allowTelemetry`.
