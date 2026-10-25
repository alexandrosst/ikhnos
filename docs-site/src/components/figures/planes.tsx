/*
 * The figures of "The parts of Ikhnos and how they fit": the two planes and where each has a part, what each plane is made of, and what
 * each part is as a piece of software. Control plane parts are drawn in the cloud colour and data plane parts in the edge colour (the
 * tier colours are only the two colours free for this), and a dashed outline always means optional.
 */
import {Arrow, Box, Fig, Lines, Pill, T, accent, cloud, edgeTier, quiet} from './kit';

const control = cloud;
const data = edgeTier;

/** A named part: a coloured outline, a bold name and up to three muted lines. */
function Part({x, y, w, h, name, lines, color, dashed, hi}: {x: number; y: number; w: number; h: number; name: string; lines: string[]; color: string; dashed?: boolean; hi?: boolean}) {
  return (
    <g>
      <Box x={x} y={y} w={w} h={h} color={color} dashed={dashed} hi={hi} />
      <T x={x + 12} y={y + 22} bold>{name}</T>
      <Lines x={x + 12} y={y + 40} lines={lines} />
    </g>
  );
}

/** The two planes, and where each of them has a part: in each cluster, in a region, at the centre. */
export function DistributedPlanes() {
  const f = 'planes';
  return (
    <Fig id={f} height={412} label="A grid of two rows and three columns. The control plane has the agent and the controller in each cluster, nothing of its own in a region, and the server at the centre. The data plane has the local operator in each cluster, the regional operator in a region, and FUSION at the centre. Telemetry runs along the data row from cluster to region to centre; the agent dials out to the server along the control row." title="Two planes, each spread over the cluster, the region and the centre" subtitle="The control plane decides and configures. The data plane carries telemetry. Neither runs through the other.">
      <T x={132} y={78} bold>In each cluster</T>
      <T x={338} y={78} bold>In a region</T>
      <T x={534} y={78} bold>At the centre</T>
      <Box x={24} y={92} w={86} h={130} color={control} />
      <T x={67} y={152} anchor="middle" bold>Control</T>
      <T x={67} y={170} anchor="middle" bold>plane</T>
      <Box x={24} y={244} w={86} h={130} color={data} />
      <T x={67} y={304} anchor="middle" bold>Data</T>
      <T x={67} y={322} anchor="middle" bold>plane</T>

      {/* the control row */}
      <Box x={124} y={92} w={188} h={130} frame />
      <Box x={136} y={104} w={164} h={50} color={control} />
      <T x={148} y={125} bold>Agent</T>
      <T x={148} y={142} small muted>senses and reports</T>
      <Box x={136} y={162} w={164} h={50} color={control} dashed />
      <T x={148} y={183} bold>Controller</T>
      <T x={148} y={200} small muted>keeps the agent installed</T>
      <Box x={326} y={92} w={188} h={130} frame dashed />
      <T x={420} y={166} anchor="middle" small muted>No part of its own.</T>
      <T x={420} y={184} anchor="middle" small muted>The server keeps a record</T>
      <T x={420} y={200} anchor="middle" small muted>of the operator here.</T>
      <Part x={528} y={92} w={208} h={130} name="Ikhnos server" lines={['Approvals, the digital twin,', 'history, the audit trail,', 'and the pages you use']} color={control} hi />

      {/* the data row */}
      <Part x={124} y={244} w={188} h={130} name="Local operator" lines={['Collectors on every node', 'and one for the cluster:', 'metrics, logs, traces']} color={data} />
      <Part x={326} y={244} w={188} h={130} name="Regional operator" lines={['A gateway that receives', 'several clusters and', 'sends them on']} color={data} dashed />
      <Part x={528} y={244} w={208} h={130} name="FUSION" lines={['The central operator at', 'the door, then the stores', 'and Grafana']} color={data} dashed />

      <Arrow fig={f} accented d="M300 129H528" />
      <T x={420} y={121} anchor="middle" small color={accent}>dials out, mutual TLS</T>
      <Arrow fig={f} accented d="M312 309H326" />
      <Arrow fig={f} accented d="M514 309H528" />
      <T x={736} y={238} anchor="end" small color={accent}>telemetry, OTLP</T>
      <Arrow fig={f} dashed d="M218 222V244" />
      <T x={226} y={238} small muted>installed by the agent chart; counters read</T>

      <T x={24} y={396} small muted>Dashed means optional. A cluster can send straight to FUSION, or to an endpoint of your own, without a regional operator.</T>
    </Fig>
  );
}

/** What the control plane is made of, and how the parts in a cluster reach each other and the server. */
export function ControlPlaneParts() {
  const f = 'ctlparts';
  return (
    <Fig id={f} height={492} label="The control plane. In a cluster, the controller and the agent never call each other: they leave notes in the IkhnosAgent resource. The controller installs the agent chart through the Kubernetes API, and the agent reads the Kubernetes API. The agent dials out to the Ikhnos server, which people reach in a browser." title="The control plane" subtitle="The server, and in each cluster an agent, a controller and a resource. The agent and the controller never call each other.">
      <Box x={24} y={64} w={506} h={374} frame />
      <T x={40} y={86} bold>In a cluster</T>
      <T x={118} y={86} small muted>one Helm release holds the agent; the controller is another</T>

      <Part x={40} y={104} w={206} h={92} name="Controller" lines={['Optional. Watches the resource', 'and installs the agent chart.', 'Never talks to the outside.']} color={control} dashed />
      <Part x={290} y={104} w={224} h={92} name="IkhnosAgent resource" lines={['Optional. A Kubernetes object.', 'spec: what the agent should be.', 'status: how it is going.']} color={control} dashed />
      <Arrow fig={f} both d="M246 150H290" />

      <Part x={40} y={262} w={206} h={92} name="Kubernetes API" lines={['The cluster’s own API.', 'The controller writes to it,', 'the agent reads from it.']} color="var(--cds-chart-axis)" />
      <Part x={290} y={262} w={224} h={92} name="Agent" lines={['One pod, read-only. Senses', 'topology, flows and health;', 'receives what is approved.']} color={control} hi />

      <Arrow fig={f} d="M143 196V262" />
      <T x={151} y={232} small muted>installs the agent chart</T>
      <Arrow fig={f} dashed d="M290 308H246" />
      <T x={268} y={298} anchor="middle" small muted>reads</T>
      <Arrow fig={f} dashed both d="M402 196V262" />
      <T x={410} y={226} small muted>marks “revoked”;</T>
      <T x={410} y={242} small muted>reads workload status</T>

      <T x={40} y={384} small muted>The resource is the note they leave for each other: the controller never</T>
      <T x={40} y={400} small muted>holds a credential for the server, and the agent never holds the right to install.</T>
      <T x={40} y={422} small muted>Without a controller, a person runs the helm command and there is no resource.</T>

      <Box x={578} y={64} w={158} h={60} />
      <T x={592} y={90} bold>You</T>
      <T x={592} y={108} small muted>browser, HTTPS</T>
      <Part x={578} y={170} w={158} h={268} name="Ikhnos server" lines={[]} color={control} hi />
      <Lines x={590} y={210} lines={['Approves each agent', 'and its access tier.', '', 'Keeps the digital twin,', 'the history and the', 'audit trail.', '', 'Advises on placement.', '', 'Writes the config each', 'agent is sent, and', 'the commands you', 'run for operators.']} />
      <Arrow fig={f} d="M657 124V170" />
      <T x={665} y={152} small muted>pages, API</T>
      <Arrow fig={f} accented both d="M514 308H578" />
      <T x={546} y={296} anchor="middle" small color={accent}>dial out</T>
      <T x={546} y={326} anchor="middle" small muted>mTLS</T>

      <T x={24} y={470} small muted>The server never calls into a cluster. Everything it sends comes back on the stream the agent opened.</T>
    </Fig>
  );
}

/** What the data plane is made of: the three tiers, and what stays out of its way. */
export function DataPlaneParts() {
  const f = 'dataparts';
  const stores: [string, string][] = [['Prometheus', 'metrics'], ['Loki', 'logs'], ['Tempo', 'traces']];
  return (
    <Fig id={f} height={544} label="The data plane. Inside a cluster the local operator has node collectors and a cluster collector. Telemetry goes to a regional operator, and from there to FUSION's central operator and its stores: Prometheus, Loki, Tempo, with Grafana reading them. A dashed line shows a local operator sending straight to FUSION. The agent only reads the local operator's counters, and the server only generates configuration and certificates." title="The data plane" subtitle="Three tiers of one kind of thing, from the cluster to the centre. They carry data and nothing else.">
      <Arrow fig={f} dashed d="M140 100V84H622V100" />
      <T x={381} y={76} anchor="middle" small muted>or straight to FUSION, or to any OTLP endpoint you run</T>

      <Box x={24} y={100} w={232} h={300} frame />
      <T x={40} y={122} bold>Local operator</T>
      <T x={40} y={138} small muted>installed by the agent chart</T>
      <Box x={40} y={152} w={200} h={78} color={data} />
      <T x={52} y={172} bold>Node collectors</T>
      <Lines x={52} y={190} lines={['A DaemonSet, one per node:', 'host and pod metrics, logs,', 'energy, GPUs']} />
      <Box x={40} y={242} w={200} h={78} color={data} />
      <T x={52} y={262} bold>Cluster collector</T>
      <Lines x={52} y={280} lines={['One Deployment: cluster state,', 'events, traces, the apps’', 'own metrics']} />
      <Box x={40} y={332} w={200} h={54} />
      <T x={52} y={352} small bold>What it scrapes</T>
      <T x={52} y={368} small muted>Kepler, DCGM, your own</T>

      <Box x={282} y={100} w={200} h={300} frame dashed />
      <T x={298} y={122} bold>Regional operator</T>
      <T x={298} y={138} small muted>optional, in a cluster you choose</T>
      <Box x={298} y={152} w={168} h={126} color={data} />
      <T x={310} y={172} bold>Gateway</T>
      <Lines x={310} y={190} lines={['One OpenTelemetry', 'collector. Takes OTLP', 'from approved clusters,', 'only with a certificate', 'its own CA signed.']} />
      <Lines x={298} y={304} lines={['No Kubernetes rights.', 'No channel to the server', 'unless you turn on the', 'content-free heartbeat.']} />

      <Box x={508} y={100} w={228} h={300} frame dashed />
      <T x={524} y={122} bold>FUSION</T>
      <T x={524} y={138} small muted>optional, part of the server’s release</T>
      <Box x={524} y={152} w={196} h={54} color={data} />
      <T x={536} y={172} bold>Central operator</T>
      <T x={536} y={190} small muted>the one door in</T>
      {stores.map(([n, s], i) => (
        <g key={n}>
          <Box x={524} y={218 + i * 30} w={196} h={26} color={data} />
          <T x={536} y={235 + i * 30} small bold>{n}</T>
          <T x={708} y={235 + i * 30} small anchor="end" muted>{s}</T>
        </g>
      ))}
      <Box x={524} y={312} w={196} h={32} />
      <T x={622} y={332} small anchor="middle">Grafana, already connected</T>
      <Lines x={524} y={364} lines={['Each store on its own volume,', 'retention set on the FUSION page.']} />

      <Arrow fig={f} accented d="M256 252H282" />
      <Arrow fig={f} accented d="M482 252H508" />

      <Box x={24} y={424} w={232} h={86} color={control} />
      <T x={36} y={444} bold>Agent (control plane)</T>
      <Lines x={36} y={462} lines={['Reads the collectors’ counters, so', 'it can say where data stops.', 'Never on the path.']} />
      <Arrow fig={f} dashed d="M140 424V400" />
      <Box x={282} y={424} w={454} h={86} color={control} />
      <T x={294} y={444} bold>Ikhnos server (control plane)</T>
      <Lines x={294} y={462} lines={['Generates each operator’s configuration and certificates.', 'It is not on the path either: the data never passes through it.']} />
      <Arrow fig={f} dashed d="M382 424V400" />
      <Arrow fig={f} dashed d="M622 424V400" />
      <T x={24} y={532} small color={quiet}>Solid arrows carry data, as OTLP. Dashed arrows are the control plane looking at, or configuring, the data plane.</T>
    </Fig>
  );
}

/** Each part as a piece of software: how it runs, whom it talks to, what it holds. */
export function ComponentAnatomy() {
  const f = 'anatomy';
  type Row = {name: string; plane: 'c' | 'd'; optional?: boolean; runs: string[]; talks: string[]; holds: string[]};
  const rows: Row[] = [
    {name: 'Server', plane: 'c', runs: ['One process, one replica,', 'one SQLite writer. Web UI', 'and API on :8080'], talks: ['Agents, who dial in on', ':8443 (gRPC, mutual TLS)', 'People, in a browser'], holds: ['Approvals, twin, history,', 'audit trail, the CA that', 'signs identities']},
    {name: 'Agent', plane: 'c', runs: ['One Deployment, one pod', 'per cluster. Read-only,', 'cluster-wide RBAC'], talks: ['The server, on one stream', 'it dials out', 'The Kubernetes API: read'], holds: ['The access tier approved', 'Its identity, in a Secret,', 'renewed at half-life']},
    {name: 'Controller', plane: 'c', optional: true, runs: ['One Deployment, in its', 'own Helm release,', 'ikhnos-controller'], talks: ['The Kubernetes API only', 'No credential for the', 'server, no address'], holds: ['The right to install the', 'agent chart, up to the', 'ceilings it was given']},
    {name: 'IkhnosAgent', plane: 'c', optional: true, runs: ['A custom resource,', 'namespaced, named', 'this-cluster'], talks: ['Nobody: it is read and', 'written by the controller', 'and the agent'], holds: ['spec: what the agent', 'should be', 'status: how it is going']},
    {name: 'Local operator', plane: 'd', runs: ['OpenTelemetry collectors', 'the agent chart installs:', 'a DaemonSet and a Deployment'], talks: ['Out only: to a regional', 'operator, FUSION or an', 'endpoint of your own'], holds: ['Permissions of their own,', 'apart from the agent’s', 'What the intent allows']},
    {name: 'Regional operator', plane: 'd', optional: true, runs: ['One OpenTelemetry', 'collector gateway, in a', 'cluster you choose'], talks: ['In: approved clusters,', 'by mutual TLS', 'Out: one OTLP endpoint'], holds: ['Its own CA', 'No Kubernetes rights', 'A record on the server']},
    {name: 'FUSION', plane: 'd', optional: true, runs: ['The central operator,', 'three stores, Grafana;', 'off until you turn it on'], talks: ['In: operators, by a', 'client certificate', 'Out: Grafana, a token API'], holds: ['Telemetry, each store on', 'its own volume', 'Retention you set']},
  ];
  const y0 = 100;
  const h = 72;
  const gap = 8;
  return (
    <Fig id={f} height={y0 + rows.length * (h + gap) + 30} label="A table of the seven parts of Ikhnos, each with how it runs, whom it talks to, and what it holds. Server, agent, controller and the IkhnosAgent resource are the control plane; local operator, regional operator and FUSION are the data plane." title="What each part is" subtitle="How it runs, whom it talks to, and what it holds. A dashed outline means optional.">
      <Pill x={24} y={58} w={118} color={control}>Control plane</Pill>
      <Pill x={152} y={58} w={96} color={data}>Data plane</Pill>
      <T x={40} y={y0 - 8} small bold>Part</T>
      <T x={190} y={y0 - 8} small bold>Runs as</T>
      <T x={372} y={y0 - 8} small bold>Talks to</T>
      <T x={554} y={y0 - 8} small bold>Holds</T>
      {rows.map((r, i) => {
        const y = y0 + i * (h + gap);
        const color = r.plane === 'c' ? control : data;
        return (
          <g key={r.name}>
            <Box x={24} y={y} w={712} h={h} color={color} dashed={r.optional} />
            <T x={40} y={y + 30} bold>{r.name}</T>
            {r.optional && (
              <T x={40} y={y + 48} small muted>optional</T>
            )}
            <Lines x={190} y={y + 22} lines={r.runs} />
            <Lines x={372} y={y + 22} lines={r.talks} />
            <Lines x={554} y={y + 22} lines={r.holds} />
          </g>
        );
      })}
    </Fig>
  );
}
