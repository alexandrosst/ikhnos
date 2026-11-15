import Link from '@docusaurus/Link';
import useBaseUrl from '@docusaurus/useBaseUrl';
import Layout from '@theme/Layout';
import ThemedImage from '@theme/ThemedImage';
import Figure from '@site/src/components/Figure';
import Shot from '@site/src/components/Shot';
import styles from './index.module.css';

const principles: [string, string][] = [
  ['Read-only', 'The agent reads the Kubernetes API and never your Secrets or ConfigMaps. Its only writes are to its own identity, its certificates and its own status. Ikhnos never deploys or moves your workloads.'],
  ['Outbound only', 'The agent opens every connection, over mutual TLS. No inbound firewall rule on a cluster, and the server never holds a kubeconfig.'],
  ['Narrow, never widen', 'Observability intent sets how far each part may look. Each layer can narrow it and none can widen it. Telemetry stays off until you turn a signal on.'],
  ['You run the changes', 'Ikhnos generates the commands and you run them. The one exception, an optional in-cluster controller, is off until you turn it on.'],
  ['Honest data', 'Every value says whether it was seen, declared or guessed, and how old it is. Missing data is named.'],
  ['Accountable', 'A person\u2019s changes are recorded with who made them, and history keeps what the landscape looked like before.'],
];

const paths: [string, string, string, string][] = [
  ['/docs/learn/what-is-ikhnos', 'New here', 'Learn what it is', 'The problem it solves, how it works in ten minutes, and the words it uses, with no prior knowledge assumed.'],
  ['/docs/journey/', 'Ready to try it', 'Your first hour', 'From an empty cluster to a connected, approved one you can explore, step by step, with what you should see after each.'],
  ['/docs/inside/', 'Want to build on it', 'Inside Ikhnos', 'How the server, the agent and the interface are put together, and why. Drawn, then explained.'],
];

const features: [string, string][] = [
  ['Topology', 'Application, Infrastructure, Map and Telemetry views of one model, with clusters tiered cloud, edge or far edge.'],
  ['Discovery that asks first', 'Connect a cluster from the UI. A code only the cluster’s own pod log shows turns “someone had a token” into “this is the agent I meant”.'],
  ['History', 'Every change is a new version with a reason attached. Ask what a cluster looked like an hour ago, or what depends on a service.'],
  ['Declared and observed', 'What a person says and what an agent sees are kept apart, and every value says which it is.'],
  ['Telemetry and FUSION', 'Opt-in collectors, regional operators and a bundled Prometheus, Loki, Tempo and Grafana, with nine dashboards.'],
  ['Cluster links', 'Loss, delay and jitter between clusters you choose to pair, from real traffic and active measurement.'],
  ['Diagnose', 'One button per agent says what is wrong, worst first, with the kubectl command for each finding.'],
  ['Accounts and audit', 'Organisations, roles, two-factor sign-in with TOTP, email code or passkeys, and an audit trail.'],
];

export default function Home() {
  const logo = useBaseUrl('/img/logo.svg');
  return (
    <Layout title="Ikhnos" description="Observability across the cloud–edge computing continuum: topology discovery and telemetry collection for your Kubernetes clusters, driven by an observability intent.">
      <header className={styles.hero}>
        <div className={styles.heroInner}>
          <div className={styles.heroText}>
            <img src={logo} alt="" className={styles.mark} />
            <h1>See everything.<br />Touch nothing.</h1>
            <p className={styles.lede}>
              Observability across the cloud–edge computing continuum: topology discovery and telemetry collection for
              your Kubernetes clusters, driven by an observability intent. One command per cluster, read-only.
            </p>
            <div className={styles.buttons}>
              <Link className="button button--primary button--lg" to="/docs/journey/">Your first hour</Link>
              <Link className="button button--secondary button--lg" to="/docs/learn/how-it-works">How it works</Link>
              <Link className={styles.ghost} to="https://github.com/alexandrosst/ikhnos">GitHub →</Link>
            </div>
            <p className={styles.note}>Open source, Apache-2.0. Pre-1.0: <Link to="/docs/project/status-and-limits">what works and what does not</Link>.</p>
          </div>
          <div className={styles.heroShot}>
            <ThemedImage
              alt="The Ikhnos topology view: a cloud cluster, a regional edge cluster and far-edge clusters with their services and the dependencies between them"
              sources={{light: useBaseUrl('/img/readme/topology-loop-light.webp'), dark: useBaseUrl('/img/readme/topology-loop-dark.webp')}}
            />
          </div>
        </div>
      </header>

      <main>
        <section className={styles.section}>
          <div className={styles.wrap}>
            <h2>Design principles</h2>
            <p className={styles.sub}>Each one is enforced by the code and the Helm chart.</p>
            <div className={styles.grid4}>
              {principles.map(([t, d]) => (
                <div key={t} className={styles.tile}>
                  <h3>{t}</h3>
                  <p>{d}</p>
                </div>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.sectionAlt}>
          <div className={styles.wrap}>
            <h2>Start where you are</h2>
            <div className={styles.grid3}>
              {paths.map(([to, kicker, title, text]) => (
                <Link key={to} to={to} className={styles.path}>
                  <span className={styles.kicker}>{kicker}</span>
                  <span className={styles.pathTitle}>{title}</span>
                  <span className={styles.pathText}>{text}</span>
                </Link>
              ))}
            </div>
          </div>
        </section>

        <section className={styles.section}>
          <div className={styles.wrap}>
            <h2>The whole thing on one page</h2>
            <p className={styles.sub}>
              One server. One small agent per cluster, which dials out. A browser. Everything else is optional.
            </p>
            <Figure name="SystemAtAGlance" maxWidth={900} caption={<>The agent always dials out. Nothing needs a hole in your firewall for the server to reach in. <Link to="/docs/learn/how-it-works">How it works in ten minutes →</Link></>} />
          </div>
        </section>

        <section className={styles.sectionAlt}>
          <div className={styles.wrap}>
            <h2>What you get</h2>
            <div className={styles.grid4}>
              {features.map(([t, d]) => (
                <div key={t} className={styles.tile}>
                  <h3>{t}</h3>
                  <p>{d}</p>
                </div>
              ))}
            </div>
          </div>
        </section>


        <section className={styles.cta}>
          <div className={styles.wrap}>
            <h2>Running in about ten minutes</h2>
            <p>One Helm command starts the server on any Kubernetes cluster, including a local k3s, kind or minikube. It makes its own encryption key; you only copy it out afterwards.</p>
            <pre className={styles.code}>{`helm install ikhnos-server oci://ghcr.io/alexandrosst/ikhnos-server \\
  --namespace ikhnos --create-namespace \\
  --set agent.service.type=NodePort \\
  --set agent.publicAddress=NODE_IP:30443`}</pre>
            <div className={styles.buttons}>
              <Link className="button button--primary button--lg" to="/docs/getting-started/quickstart">Quickstart</Link>
              <Link className="button button--secondary button--lg" to="/docs/journey/">The guided journey</Link>
            </div>
          </div>
        </section>
      </main>
    </Layout>
  );
}
