---
id: connect-a-cluster
title: "4. Connect a cluster"
description: Use the Connect a cluster wizard to get the one install command for an agent, understand what it contains, and run it.
---

<StepBar n={4} of={7} title="Connect a cluster" />

# Connect a cluster

A cluster joins Ikhnos by running a small agent. You do not create the agent anywhere by hand: a wizard in the interface prints the exact command, already filled in with this server's address, the fingerprint of its certificate authority, and a one-time token.

## Open the wizard

On the empty Topology page, press **Connect a cluster**. It is also on the **Agents** page, and it is the first step of the checklist.

<Shot src="journey/connect-name" alt="The Connect a cluster dialog with a Cluster name field, the Access cards and the Collect more switches below it" caption="Give the cluster a name as you want to see it. It can be renamed later." />

Type a name, such as `edge-lab`. Everything the agent can be asked to do is on the same page, in three groups, with nothing folded away. The defaults are the recommended access level and nothing else switched on, so you can press **Create install command** straight away.

## Choose what the agent may read (optional)

You can press **Create install command** now and come back to this later. The choices are these.

<Shot src="journey/connect-options" alt="The Access cards for Infrastructure and Services, then the Collect more switches and the namespace limit" caption="Two access levels, then what to collect on top, then what to leave out." />

**Access** is what the agent may read. It is always read-only: it can never read Secrets or ConfigMaps, or change anything in the cluster. There are two choices.

- **Infrastructure** is the nodes, storage classes and ingress classes: what the cluster is made of.
- **Services** (the default) is everything in Infrastructure plus namespaces, workloads, pods, services and ingresses: what runs on it.

This is the **ceiling**. It is enforced by Kubernetes itself, through the permissions the chart creates, and only someone with `helm upgrade` rights on the cluster can raise it later.

<Figure name="TrustChain" caption="The ceiling you choose here is the top of the chain. Later, an administrator can approve less, and the agent can send less, but never more." />

**Collect more** is independent of the access level. Each row is a switch, off until you turn it on, and each says what it puts in the cluster: a pod on each node, or nothing extra because it runs inside the agent. You can add any of them after the install, so it is fine to start with none.

- **Node hardware and network** (a pod on each node) tells virtual machines from bare metal, and reports the device model, the disks, and the uplinks and tunnels. It never reports serial numbers, MAC addresses or processes. It needs the host's `/sys` and network, so a namespace that enforces the Pod Security baseline must be labelled privileged.
- **Traffic between services** (a pod on each node, with extra privileges) counts connections and bytes using eBPF or the kernel's connection table, which is what lets Ikhnos draw real dependencies. It asks for more than the others, so read what it is allowed to do before you switch it on.
- **Round-trip times** (runs in the agent) times TCP connections to a few outside addresses, to measure the paths between clusters. On its own it times the addresses you add on the **Sites** page; with **Traffic between services** also on, it can offer the ones your workloads call most.

The traffic and round-trip rows need the **Services** level. If you choose **Infrastructure** they stay in view, dimmed and off, with a note saying what they need.

**Limit what it reports** is a narrower view, not a permission. **Only some namespaces** lets you name the namespaces to report, the ones never to report, or a label to select them by. What is left out never leaves the cluster.

## Run the command

Press **Create install command**.

<Shot src="journey/connect-command" alt="The wizard showing the helm install command for the agent, a note that the token works once, and the message Waiting for the agent to start and connect" caption="The command, ready to copy. The enrollment token is shown only here and cannot be retrieved later. (Here the token is replaced by a placeholder.)" />

The command looks like this. Yours will have real values.

```bash
helm install ikhnos-agent oci://ghcr.io/alexandrosst/ikhnos-agent \
  --namespace ikhnos-system --create-namespace \
  --set server.address=NODE_IP:30443 \
  --set enrollment.key=sha256/…=.ikh_…
```

It contains the address the agent will dial, and one value, `enrollment.key`, that holds two things joined by a dot: the **CA pin** (the fingerprint of the server's certificate authority, so the agent can tell it reached the right server before it trusts anything) and the **one-time token** (valid for an hour, one use).

Copy it with **Copy command** and run it where `kubectl` and `helm` point at the cluster you are connecting. For your first try, that is the same cluster as before.

<See>
Helm reports `STATUS: deployed`. Then `kubectl -n ikhnos-system get pods` shows an `ikhnos-agent` pod that becomes `Running`. In the wizard, the second of five steps, **Agent connects**, is the one in progress, and the spinner reads that it is waiting for the agent to start and connect. Most agents connect within a minute. If yours has not after two minutes, a **Still waiting?** box appears under the spinner with the commands to check: the pod and its log for a helm install, or the `IkhnosAgent` resource the controller made (with the command that raises the controller's ceiling, if it was installed earlier with a lower one) for the controller.
</See>

The wizard has five steps, shown in a row along the top from the first screen: **Set up** (this form), **Agent connects**, **You approve it**, **Discovering**, and **Connected**. It tracks them for you, and **Continue in background** lets you close it and come back; the checklist and the Agents page show the same progress.

## If it did not work

| You see | Usually means | What to do |
|---|---|---|
| The pod is `ImagePullBackOff` | The node cannot pull from `ghcr.io`. | Same checks as in step 2. |
| The pod runs but the wizard never moves | The agent cannot reach the server's address, or the pin does not match. | `kubectl -n ikhnos-system logs deploy/ikhnos-agent` names the failure. See "The agent never appears" in [Common errors](../troubleshooting/common-errors.md). |
| `helm install` says the token is invalid or used | The token works once and for an hour. | Press **Connect a cluster** again for a fresh command. |

## What just happened

The agent started, dialled the server, checked the fingerprint, and sent the token with a request for a certificate. It is now waiting, and the server has not trusted it yet. That is deliberate. The next step is what makes it trusted.

[Next: approve it →](./approve.md)
