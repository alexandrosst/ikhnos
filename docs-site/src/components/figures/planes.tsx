/*
 * The figures of "The parts of Ikhnos and how they fit": the two planes and where each has a part, what each plane is made of, and what
 * each part is as a piece of software. Control plane parts are drawn in the cloud colour and data plane parts in the edge colour (the two tier
 * colours that are free for this), every part has a small icon so it can be found again from one figure to the next, and a dashed outline
 * always means optional.
 */
import type {ReactNode} from 'react';
import {Arrow, Fig, Lines, Pill, T, accent, cloud, edge, edgeTier, quiet, rule} from './kit';

const control = cloud;
const data = edgeTier;

export type IconKind = 'server' | 'agent' | 'controller' | 'resource' | 'local' | 'regional' | 'fusion' | 'api' | 'person';

/** A 24-unit pictogram centred on (cx, cy), drawn as strokes in one colour. Same icon, same part, in every figure. */
function Icon({kind, cx, cy, color}: {kind: IconKind; cx: number; cy: number; color: string}) {
  const p = {fill: 'none', stroke: color, strokeWidth: 1.6, strokeLinecap: 'round' as const, strokeLinejoin: 'round' as const};
  let body: ReactNode;
  switch (kind) {
    case 'server':
      body = (
        <>
          <rect x="-8" y="-9" width="16" height="5.5" rx="1.5" {...p} />
          <rect x="-8" y="-2.75" width="16" height="5.5" rx="1.5" {...p} />
          <rect x="-8" y="3.5" width="16" height="5.5" rx="1.5" {...p} />
          <path d="M-4.5 -6.2h.01M-4.5 0h.01M-4.5 6.2h.01" {...p} strokeWidth={2.2} />
        </>
      );
      break;
    case 'agent':
      body = (
        <>
          <path d="M-10 0Q0 -9 10 0Q0 9 -10 0Z" {...p} />
          <circle cx="0" cy="0" r="3" {...p} />
        </>
      );
      break;
    case 'controller':
      body = (
        <>
          <path d="M7.5 -3.5A8 8 0 1 0 8 2" {...p} />
          <path d="M8.5 -9.5V-3.5H2.5" {...p} />
        </>
      );
      break;
    case 'resource':
      body = (
        <>
          <path d="M-6.5 -9.5H2L7 -4.5V9.5H-6.5Z" {...p} />
          <path d="M2 -9.5V-4.5H7M-3 0H3.5M-3 3.5H3.5" {...p} />
        </>
      );
      break;
    case 'local':
      body = <path d="M-9 -8H9L2 0.5V8.5L-2 6V0.5Z" {...p} />;
      break;
    case 'regional':
      body = (
        <>
          <path d="M-10 -6H-4L2 0M-10 6H-4L2 0M2 0H9.5" {...p} />
          <path d="M6 -3.5L9.5 0L6 3.5" {...p} />
        </>
      );
      break;
    case 'fusion':
      body = (
        <>
          <ellipse cx="0" cy="-6" rx="8" ry="3" {...p} />
          <path d="M-8 -6V6C-8 7.8 -4.4 9 0 9S8 7.8 8 6V-6M-8 0C-8 1.8 -4.4 3 0 3S8 1.8 8 0" {...p} />
        </>
      );
      break;
    case 'api':
      body = (
        <>
          <path d="M0 -9.5L8.5 -4.75V4.75L0 9.5L-8.5 4.75V-4.75Z" {...p} />
          <path d="M0 -9.5V0M0 0L8.5 4.75M0 0L-8.5 4.75" {...p} />
        </>
      );
      break;
    case 'person':
      body = (
        <>
          <circle cx="0" cy="-4.5" r="4" {...p} />
          <path d="M-8.5 9C-8.5 3.5 -4.5 2 0 2S8.5 3.5 8.5 9" {...p} />
        </>
      );
      break;
  }
  return <g transform={`translate(${cx} ${cy})`}>{body}</g>;
}

type CardProps = {
  x: number;
  y: number;
  w: number;
  h: number;
  name: string;
  /** One line under the name. */
  sub?: string;
  /** Body lines, under the header. */
  lines?: string[];
  color: string;
  icon: IconKind;
  dashed?: boolean;
  /** Accent outline: the thing this figure is about. */
  hi?: boolean;
  children?: ReactNode;
};

/** A part: a tinted card, a round icon badge, the name, a line under it, and what it does. */
function Card({x, y, w, h, name, sub, lines, color, icon, dashed, hi, children}: CardProps) {
  return (
    <g>
      <rect x={x} y={y} width={w} height={h} rx="10" fill={color} fillOpacity={hi ? 0.14 : 0.07} stroke={hi ? accent : color} strokeWidth={hi ? 2 : 1.5} strokeDasharray={dashed ? '5 4' : undefined} />
      <circle cx={x + 25} cy={y + 25} r="15" fill={color} fillOpacity="0.16" />
      <Icon kind={icon} cx={x + 25} cy={y + 25} color={color} />
      <T x={x + 49} y={y + (sub ? 22 : 29)} bold>{name}</T>
      {sub && <T x={x + 49} y={y + 38} small muted>{sub}</T>}
      {lines && <Lines x={x + 14} y={y + 58} lines={lines} />}
      {children}
    </g>
  );
}

/** A plain outlined box for something that is not a part of Ikhnos (the Kubernetes API, a browser). */
function Other({x, y, w, h, name, sub, lines, icon}: {x: number; y: number; w: number; h: number; name: string; sub?: string; lines?: string[]; icon: IconKind}) {
  return <Card x={x} y={y} w={w} h={h} name={name} sub={sub} lines={lines} color={edge} icon={icon} />;
}

/** A band that holds one plane's row, with its name in the left margin. */
function Band({y, h, color, name, sub, icon}: {y: number; h: number; color: string; name: string; sub: string[]; icon: IconKind}) {
  const cx = 76;
  return (
    <g>
      <rect x="24" y={y} width="712" height={h} rx="14" fill={color} fillOpacity="0.055" stroke={color} strokeOpacity="0.35" strokeWidth="1" />
      <rect x="24" y={y} width="5" height={h} rx="2.5" fill={color} />
      <circle cx={cx} cy={y + h / 2 - 28} r="17" fill={color} fillOpacity="0.16" />
      <Icon kind={icon} cx={cx} cy={y + h / 2 - 28} color={color} />
      <T x={cx} y={y + h / 2 + 4} anchor="middle" bold>{name}</T>
      <Lines x={cx} y={y + h / 2 + 22} lines={sub} anchor="middle" />
    </g>
  );
}

/** The two planes, and where each of them has a part: in each cluster, in a region, at the centre. */
export function DistributedPlanes() {
  const f = 'planes';
  const c1 = {x: 128, w: 184};
  const c2 = {x: 340, w: 168};
  const c3 = {x: 536, w: 188};
  return (
    <Fig id={f} height={440} label="A grid of two rows and three columns. The control plane has the agent and the controller in each cluster, nothing of its own in a region, and the server at the centre. The data plane has the local operator in each cluster, the regional operator in a region, and FUSION at the centre. Telemetry runs along the data row from cluster to region to centre; the agent dials out to the server along the control row." title="Two planes, each spread over the cluster, the region and the centre" subtitle="The control plane decides and configures. The data plane carries telemetry. Neither runs through the other.">
      <T x={c1.x + c1.w / 2} y={80} anchor="middle" bold>In each cluster</T>
      <T x={c2.x + c2.w / 2} y={80} anchor="middle" bold>In a region</T>
      <T x={c3.x + c3.w / 2} y={80} anchor="middle" bold>At the centre</T>

      <Band y={92} h={134} color={control} name="Control plane" sub={['decides and', 'configures']} icon="server" />
      <Band y={256} h={134} color={data} name="Data plane" sub={['carries', 'telemetry']} icon="local" />

      {/* the control row */}
      <Card x={c1.x} y={104} w={c1.w} h={52} name="Agent" sub="senses and reports" color={control} icon="agent" />
      <Card x={c1.x} y={164} w={c1.w} h={52} name="Controller" sub="keeps the agent installed" color={control} icon="controller" dashed />
      <rect x={c2.x} y={104} width={c2.w} height={112} rx="10" fill="none" stroke={rule} strokeWidth="1.5" strokeDasharray="5 4" />
      <T x={c2.x + c2.w / 2} y={170} anchor="middle" small muted>No part of its own.</T>
      <T x={c2.x + c2.w / 2} y={188} anchor="middle" small muted>The server keeps a record</T>
      <T x={c2.x + c2.w / 2} y={204} anchor="middle" small muted>of the operator here.</T>
      <Card x={c3.x} y={104} w={c3.w} h={112} name="Ikhnos server" lines={['Approvals, the twin,', 'history, the audit', 'trail and the pages']} color={control} icon="server" hi />

      {/* the data row */}
      <Card x={c1.x} y={268} w={c1.w} h={110} name="Local operator" lines={['Collectors on every node', 'and one for the cluster']} color={data} icon="local" />
      <Card x={c2.x} y={268} w={c2.w} h={110} name="Regional operator" lines={['Receives several', 'clusters, sends them on']} color={data} icon="regional" dashed />
      <Card x={c3.x} y={268} w={c3.w} h={110} name="FUSION" lines={['The central operator at', 'the door, then the stores', 'and Grafana']} color={data} icon="fusion" dashed />

      <Arrow fig={f} accented d="M312 130H536" />
      <T x={424} y={122} anchor="middle" small color={accent}>dials out, mutual TLS</T>
      <Arrow fig={f} accented d="M312 323H340" />
      <Arrow fig={f} accented d="M508 323H536" />
      <T x={724} y={250} anchor="end" small color={accent}>telemetry, as OTLP</T>
      <Arrow fig={f} dashed d="M220 226V256" />
      <T x={230} y={245} small muted>the chart installs it; the agent reads its counters</T>

      <T x={24} y={420} small muted>Dashed means optional. A cluster can send straight to FUSION, or to an endpoint of your own, without a regional operator.</T>
    </Fig>
  );
}

/** What the control plane is made of, and how the parts in a cluster reach each other and the server. */
export function ControlPlaneParts() {
  const f = 'ctlparts';
  return (
    <Fig id={f} height={500} label="The control plane. In a cluster, the controller and the agent never call each other: they leave notes in the IkhnosAgent resource. The controller installs the agent chart through the Kubernetes API, and the agent reads the Kubernetes API. The agent dials out to the Ikhnos server, which people reach in a browser." title="The control plane" subtitle="The server, and in each cluster an agent, a controller and a resource. The agent and the controller never call each other.">
      <rect x="24" y="64" width="506" height="388" rx="14" fill={control} fillOpacity="0.04" stroke={rule} strokeWidth="1.25" />
      <T x={40} y={86} bold>In a cluster</T>
      <T x={130} y={86} small muted>the agent is one Helm release, the controller another</T>

      <Card x={40} y={104} w={206} h={92} name="Controller" sub="optional" lines={['Watches the resource and', 'installs the agent chart.']} color={control} icon="controller" dashed />
      <Card x={290} y={104} w={224} h={92} name="IkhnosAgent resource" sub="optional" lines={['spec: what the agent should be.', 'status: how it is going.']} color={control} icon="resource" dashed />
      <Arrow fig={f} both d="M246 150H290" />

      <Other x={40} y={262} w={206} h={92} name="Kubernetes API" lines={['The controller writes to it;', 'the agent only reads.']} icon="api" />
      <Card x={290} y={262} w={224} h={92} name="Agent" sub="one pod, read-only" lines={['Senses topology, flows and', 'health; receives what is approved.']} color={control} icon="agent" hi />

      <Arrow fig={f} d="M143 196V262" />
      <T x={151} y={232} small muted>installs the agent chart</T>
      <Arrow fig={f} dashed d="M290 308H246" />
      <T x={268} y={298} anchor="middle" small muted>reads</T>
      <Arrow fig={f} dashed both d="M402 196V262" />
      <T x={410} y={226} small muted>marks “revoked”;</T>
      <T x={410} y={242} small muted>reads workload status</T>

      <T x={40} y={384} small muted>The resource is the note they leave for each other. The controller holds no</T>
      <T x={40} y={400} small muted>credential for the server, and the agent never holds the right to install.</T>
      <T x={40} y={424} small muted>Without a controller, a person runs the helm command and there is no resource.</T>

      <Other x={578} y={64} w={158} h={60} name="You" sub="browser, HTTPS" icon="person" />
      <Card x={578} y={170} w={158} h={282} name="Ikhnos server" color={control} icon="server" hi>
        <Lines x={592} y={222} lines={['Approves each agent', 'and its access tier.', '', 'Keeps the digital twin,', 'the history and the', 'audit trail.', '', 'Advises on placement.', '', 'Writes the config each', 'agent is sent, and the', 'commands you run for', 'operators.']} />
      </Card>
      <Arrow fig={f} d="M657 124V170" />
      <T x={665} y={152} small muted>pages, API</T>
      <Arrow fig={f} accented both d="M514 308H578" />
      <T x={546} y={296} anchor="middle" small color={accent}>dial out</T>
      <T x={546} y={326} anchor="middle" small muted>mTLS</T>

      <T x={24} y={482} small muted>The server never calls into a cluster. Everything it sends comes back on the stream the agent opened.</T>
    </Fig>
  );
}

/** What the data plane is made of: the three tiers, and what stays out of its way. */
export function DataPlaneParts() {
  const f = 'dataparts';
  const stores: [string, string][] = [['Prometheus', 'metrics'], ['Loki', 'logs'], ['Tempo', 'traces']];
  return (
    <Fig id={f} height={548} label="The data plane. Inside a cluster the local operator has node collectors and a cluster collector. Telemetry goes to a regional operator, and from there to FUSION's central operator and its stores: Prometheus, Loki, Tempo, with Grafana reading them. A dashed line shows a local operator sending straight to FUSION. The agent only reads the local operator's counters, and the server only generates configuration and certificates." title="The data plane" subtitle="Three tiers of one kind of thing, from the cluster to the centre. They carry data and nothing else.">
      <Arrow fig={f} dashed d="M140 100V84H622V100" />
      <T x={381} y={76} anchor="middle" small muted>or straight to FUSION, or to any OTLP endpoint you run</T>

      <rect x="24" y="100" width="232" height="300" rx="14" fill={data} fillOpacity="0.04" stroke={rule} strokeWidth="1.25" />
      <T x={40} y={122} bold>Local operator</T>
      <T x={40} y={138} small muted>installed by the agent chart</T>
      <Card x={40} y={150} w={200} h={104} name="Node collectors" lines={['A DaemonSet, one per node:', 'host and pod metrics, logs,', 'energy, GPUs']} color={data} icon="local" />
      <Card x={40} y={262} w={200} h={104} name="Cluster collector" lines={['One Deployment: cluster', 'state, events, traces, the', 'apps’ own metrics']} color={data} icon="local" />
      <T x={40} y={388} small muted>Scrapes Kepler, DCGM and more</T>

      <rect x="282" y="100" width="200" height="300" rx="14" fill={data} fillOpacity="0.04" stroke={rule} strokeWidth="1.25" strokeDasharray="5 4" />
      <T x={298} y={122} bold>Regional operator</T>
      <T x={298} y={138} small muted>optional, in a cluster you choose</T>
      <Card x={298} y={152} w={168} h={136} name="Gateway" lines={['One OpenTelemetry', 'collector. Takes OTLP', 'from approved clusters,', 'only with a certificate', 'its own CA signed.']} color={data} icon="regional" />
      <Lines x={298} y={316} lines={['No Kubernetes rights.', 'No channel to the server', 'unless you turn on the', 'content-free heartbeat.']} />

      <rect x="508" y="100" width="228" height="300" rx="14" fill={data} fillOpacity="0.04" stroke={rule} strokeWidth="1.25" strokeDasharray="5 4" />
      <T x={524} y={122} bold>FUSION</T>
      <T x={524} y={138} small muted>optional, part of the server’s release</T>
      <Card x={524} y={152} w={196} h={56} name="Central operator" sub="the one door in" color={data} icon="regional" />
      {stores.map(([n, s], i) => (
        <g key={n}>
          <rect x={524} y={218 + i * 30} width={196} height={26} rx="8" fill={data} fillOpacity="0.07" stroke={data} strokeWidth="1.5" />
          <Icon kind="fusion" cx={540} cy={231 + i * 30} color={data} />
          <T x={558} y={235 + i * 30} small bold>{n}</T>
          <T x={708} y={235 + i * 30} small anchor="end" muted>{s}</T>
        </g>
      ))}
      <Other x={524} y={312} w={196} h={42} name="Grafana" icon="api" />
      <Lines x={524} y={376} lines={['Each store on its own volume,', 'retention set on the FUSION page.']} />

      <Arrow fig={f} accented d="M256 252H282" />
      <Arrow fig={f} accented d="M482 252H508" />

      <Card x={24} y={424} w={232} h={86} name="Agent" sub="control plane" lines={['Reads the collectors’ counters, so it', 'can say where data stops.']} color={control} icon="agent" />
      <Arrow fig={f} dashed d="M140 424V400" />
      <Card x={282} y={424} w={454} h={86} name="Ikhnos server" sub="control plane" lines={['Generates each operator’s configuration and certificates.', 'It is not on the path either: the data never passes through it.']} color={control} icon="server" />
      <Arrow fig={f} dashed d="M382 424V400" />
      <Arrow fig={f} dashed d="M622 424V400" />
      <T x={24} y={534} small color={quiet}>Solid arrows carry data, as OTLP. Dashed arrows are the control plane looking at, or configuring, the data plane.</T>
    </Fig>
  );
}

type Row = {name: string; icon: IconKind; plane: 'c' | 'd'; optional?: boolean; runs: string[]; talks: string[]; holds: string[]};

const ROWS: Row[] = [
  {name: 'Server', icon: 'server', plane: 'c', runs: ['One process, one replica,', 'one SQLite writer. Web UI', 'and API on :8080'], talks: ['Agents, who dial in on', ':8443 (gRPC, mutual TLS)', 'People, in a browser'], holds: ['Approvals, twin, history,', 'audit trail, the CA that', 'signs identities']},
  {name: 'Agent', icon: 'agent', plane: 'c', runs: ['One Deployment, one pod', 'per cluster. Read-only,', 'cluster-wide RBAC'], talks: ['The server, on one stream', 'it dials out', 'The Kubernetes API: read'], holds: ['The access tier approved', 'Its identity, in a Secret,', 'renewed at half-life']},
  {name: 'Controller', icon: 'controller', plane: 'c', optional: true, runs: ['One Deployment, in its', 'own Helm release,', 'ikhnos-controller'], talks: ['The Kubernetes API only', 'No credential for the', 'server, no address'], holds: ['The right to install the', 'agent chart, up to the', 'ceilings it was given']},
  {name: 'IkhnosAgent', icon: 'resource', plane: 'c', optional: true, runs: ['A custom resource,', 'namespaced, named', 'this-cluster'], talks: ['Nobody: it is read and', 'written by the controller', 'and the agent'], holds: ['spec: what the agent', 'should be', 'status: how it is going']},
  {name: 'Local operator', icon: 'local', plane: 'd', runs: ['Collectors the agent', 'chart installs: a', 'DaemonSet, a Deployment'], talks: ['Out only: to a regional', 'operator, FUSION or an', 'endpoint of your own'], holds: ['Permissions of their own,', 'apart from the agent’s', 'What the intent allows']},
  {name: 'Regional operator', icon: 'regional', plane: 'd', optional: true, runs: ['One OpenTelemetry', 'collector gateway, in a', 'cluster you choose'], talks: ['In: approved clusters,', 'by mutual TLS', 'Out: one OTLP endpoint'], holds: ['Its own CA', 'No Kubernetes rights', 'A record on the server']},
  {name: 'FUSION', icon: 'fusion', plane: 'd', optional: true, runs: ['The central operator,', 'three stores, Grafana;', 'off until you turn it on'], talks: ['In: operators, by a', 'client certificate', 'Out: Grafana, a token API'], holds: ['Telemetry, each store on', 'its own volume', 'Retention you set']},
];

/** Each part as a piece of software: how it runs, whom it talks to, what it holds. */
export function ComponentAnatomy() {
  const f = 'anatomy';
  const y0 = 108;
  const h = 72;
  const gap = 8;
  return (
    <Fig id={f} height={y0 + ROWS.length * (h + gap) + 24} label="A table of the seven parts of Ikhnos, each with how it runs, whom it talks to, and what it holds. Server, agent, controller and the IkhnosAgent resource are the control plane; local operator, regional operator and FUSION are the data plane." title="What each part is" subtitle="How it runs, whom it talks to, and what it holds. A dashed outline means optional.">
      <Pill x={24} y={60} w={118} color={control}>Control plane</Pill>
      <Pill x={152} y={60} w={96} color={data}>Data plane</Pill>
      <T x={86} y={y0 - 8} small bold>Part</T>
      <T x={216} y={y0 - 8} small bold>Runs as</T>
      <T x={390} y={y0 - 8} small bold>Talks to</T>
      <T x={564} y={y0 - 8} small bold>Holds</T>
      {ROWS.map((r, i) => {
        const y = y0 + i * (h + gap);
        const color = r.plane === 'c' ? control : data;
        return (
          <g key={r.name}>
            <rect x={24} y={y} width={712} height={h} rx="10" fill={color} fillOpacity="0.06" stroke={color} strokeWidth="1.5" strokeDasharray={r.optional ? '5 4' : undefined} />
            <rect x={24} y={y} width={5} height={h} rx="2.5" fill={color} />
            <circle cx={58} cy={y + h / 2 - 4} r="16" fill={color} fillOpacity="0.16" />
            <Icon kind={r.icon} cx={58} cy={y + h / 2 - 4} color={color} />
            <T x={86} y={y + 32} bold>{r.name}</T>
            {r.optional && <T x={86} y={y + 50} small muted>optional</T>}
            <Lines x={216} y={y + 24} lines={r.runs} />
            <Lines x={390} y={y + 24} lines={r.talks} />
            <Lines x={564} y={y + 24} lines={r.holds} />
          </g>
        );
      })}
    </Fig>
  );
}
