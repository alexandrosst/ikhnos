#!/usr/bin/env python3
"""Made-up telemetry for the Grafana video (node record-videos.mjs grafana): three clusters, their nodes, pods and logs, pushed over OTLP into FUSION's Prometheus and Loki.
Needs pip install opentelemetry-proto requests, and the stores reachable (see PROM and LOKI below). Backfills the given minutes (default 190) in order, then keeps writing every 30 s.
For a clean backfill start from empty stores (they refuse samples older than what they already hold), with Prometheus out_of_order_time_window above the backfill (the chart sets 30m)."""
import math, random, sys, time, requests
from opentelemetry.proto.collector.metrics.v1.metrics_service_pb2 import ExportMetricsServiceRequest
from opentelemetry.proto.collector.logs.v1.logs_service_pb2 import ExportLogsServiceRequest
from opentelemetry.proto.metrics.v1 import metrics_pb2
from opentelemetry.proto.logs.v1 import logs_pb2
from opentelemetry.proto.common.v1.common_pb2 import KeyValue, AnyValue
from opentelemetry.proto.resource.v1.resource_pb2 import Resource

import os
ATH = os.environ.get('IKHNOS_SAMPLE_ATHENS_ID', 'cl-athens-edge')  # the id of the cluster a real agent connected, so it is one cluster in Grafana, not two
PROM = 'http://ikhnos-fusion-prometheus.ikhnos-system.svc:9090/api/v1/otlp/v1/metrics'
LOKI = 'http://ikhnos-fusion-loki.ikhnos-system.svc:3100/otlp/v1/logs'
STEP = 30
BACKFILL_MIN = int(sys.argv[1]) if len(sys.argv) > 1 else 190
rnd = random.Random(7)

def kv(k, v):
    return KeyValue(key=k, value=AnyValue(string_value=str(v)) if not isinstance(v, int) else AnyValue(int_value=v))

def res(**a):
    return Resource(attributes=[kv(k.replace('__', '.').replace('_', '.') if False else k, v) for k, v in a.items()])

CLUSTERS = {
    'cl-berlin-core': dict(name='berlin-core', nodes=['core-1', 'core-2', 'core-3'], cpu=8, mem=16),
    ATH: dict(name='athens-edge', nodes=['athens-edge-1', 'athens-edge-2'], cpu=4, mem=8),
    'cl-paris-site': dict(name='paris-site', nodes=['paris-1', 'paris-2', 'paris-3'], cpu=4, mem=8),
}
WORK = {  # cluster -> [(namespace, kind, name, replicas, cpu cores per pod, mem MiB per pod)]
    'cl-berlin-core': [('shop', 'deployment', 'web', 3, .12, 180), ('shop', 'deployment', 'api', 2, .25, 320), ('shop', 'deployment', 'cart', 2, .08, 150), ('shop', 'statefulset', 'db', 1, .4, 900), ('kube-system', 'deployment', 'coredns', 2, .02, 40), ('ikhnos-system', 'deployment', 'ikhnos-telemetry-gateway', 1, .06, 130)],
    ATH: [('shop', 'deployment', 'web', 2, .1, 160), ('shop', 'deployment', 'sensor-gateway', 2, .15, 220), ('kube-system', 'deployment', 'coredns', 1, .02, 40), ('ikhnos-system', 'deployment', 'ikhnos-telemetry-gateway', 1, .05, 120)],
    'cl-paris-site': [('shop', 'deployment', 'web', 2, .11, 170), ('shop', 'deployment', 'api', 2, .22, 300), ('kube-system', 'deployment', 'coredns', 1, .02, 40), ('ikhnos-system', 'deployment', 'ikhnos-telemetry-gateway', 1, .05, 120)],
}
APPS = {'shop': ['web', 'api', 'cart', 'db', 'sensor-gateway']}
PODS = []  # (cluster, ns, kind, workload, pod, node)
for cid, ws in WORK.items():
    nodes = CLUSTERS[cid]['nodes']; i = 0
    for ns, kind, name, n, cpu, mem in ws:
        for r in range(n):
            pod = f'{name}-{r}' if kind == 'statefulset' else f'{name}-{rnd.choice("abcdefghjk")}{rnd.randint(10000,99999):x}-{rnd.choice("xyzwq")}{rnd.randint(100,999)}'
            PODS.append((cid, ns, kind, name, pod, nodes[i % len(nodes)], cpu, mem)); i += 1
PENDING = ('cl-paris-site', 'api')  # one pod that is still starting

def ts(t): return int(t * 1e9)
def gauge(name, unit, points):
    return metrics_pb2.Metric(name=name, unit=unit, gauge=metrics_pb2.Gauge(data_points=points))
def counter(name, unit, points):
    return metrics_pb2.Metric(name=name, unit=unit, sum=metrics_pb2.Sum(data_points=points, aggregation_temporality=2, is_monotonic=True))
def dp(t, v, **attrs):
    p = metrics_pb2.NumberDataPoint(time_unix_nano=ts(t), start_time_unix_nano=ts(t - 3600 * 6), attributes=[kv(k, x) for k, x in attrs.items()])
    if isinstance(v, int) and not isinstance(v, bool): p.as_int = v
    else: p.as_double = float(v)
    return p
def rm(resource, metrics):
    return metrics_pb2.ResourceMetrics(resource=Resource(attributes=[kv(k, v) for k, v in resource.items()]), scope_metrics=[metrics_pb2.ScopeMetrics(metrics=metrics)])

net = {}  # running counters
def wave(t, phase, period=5400.0, lo=.35, hi=.8): return lo + (hi - lo) * (0.5 + 0.5 * math.sin(2 * math.pi * t / period + phase))

def metrics_at(t):
    out = []
    for ci, (cid, c) in enumerate(CLUSTERS.items()):
        base = {'ikhnos.cluster.id': cid, 'ikhnos.org.id': 'default', 'k8s.cluster.name': c['name'], 'ikhnos.operator.id': 'op-eu', 'ikhnos.operator.name': 'eu-gateway'}
        out.append(rm({'ikhnos.cluster.id': cid, 'ikhnos.org.id': 'default'}, [gauge('ikhnos_cluster_info', '', [dp(t, 1, cluster=cid, cluster_name=c['name'])])]))
        out.append(rm(base, [gauge('up', '', [dp(t, 1)]), gauge('otelcol_exporter_queue_size', '', [dp(t, int(rnd.random() * 6), exporter='otlp')]), gauge('otelcol_exporter_queue_capacity', '', [dp(t, 1000, exporter='otlp')])]))
        for ni, n in enumerate(c['nodes']):
            r = dict(base, **{'k8s.node.name': n})
            u = wave(t, ni * 1.3 + ci * .7) * c['cpu'] * .55
            m = c['mem'] * 2**30 * (0.42 + 0.18 * wave(t, ni + ci, 7200, 0, 1))
            for d, rate in (('receive', 2.4e5), ('transmit', 1.6e5)):
                k = (cid, n, d); net[k] = net.get(k, 0) + rate * STEP * (0.6 + 0.8 * wave(t, ni + (d == 'transmit'), 1800, 0, 1))
            out.append(rm(r, [
                gauge('k8s.node.condition_ready', '{condition}', [dp(t, 1)]),
                gauge('k8s.node.cpu.usage', '{cpu}', [dp(t, u)]),
                gauge('k8s.node.allocatable.cpu', '{cpu}', [dp(t, c['cpu'])]),
                gauge('k8s.node.memory.working_set', 'By', [dp(t, m)]),
                gauge('k8s.node.allocatable.memory', 'By', [dp(t, c['mem'] * 2**30)]),
                gauge('k8s.node.filesystem.usage', 'By', [dp(t, 40 * 2**30 + ni * 5 * 2**30 + (t % 86400) * 2000)]),
                gauge('k8s.node.filesystem.capacity', 'By', [dp(t, 120 * 2**30)]),
                counter('k8s.node.network.io', 'By', [dp(t, int(net[(cid, n, d)]), direction=d) for d in ('receive', 'transmit')]),
            ]))
    for (cid, ns, kind, wl, pod, node, cpu, mem) in PODS:
        c = CLUSTERS[cid]
        r = {'ikhnos.cluster.id': cid, 'ikhnos.org.id': 'default', 'k8s.cluster.name': c['name'], 'k8s.namespace.name': ns, 'k8s.pod.name': pod, 'k8s.node.name': node, 'k8s.%s.name' % kind: wl, 'ikhnos.operator.id': 'op-eu', 'ikhnos.operator.name': 'eu-gateway'}
        h = sum(map(ord, pod))
        phase = 1 if (cid, wl) == PENDING and pod.endswith(str(h % 10)) is False and pod == [p[4] for p in PODS if p[0] == cid and p[3] == wl][-1] else 2
        for d in ('receive', 'transmit'):
            k = (pod, d); net[k] = net.get(k, 0) + (9e3 if d == 'receive' else 6e3) * STEP * (0.5 + wave(t, h, 1500, 0, 1))
        ms = []
        ms.append(gauge('k8s.pod.phase', '', [dp(t, phase)]))
        if phase == 2:
            ms += [gauge('k8s.pod.cpu.usage', '{cpu}', [dp(t, cpu * (0.5 + wave(t, h % 7, 2400, 0, 1)))]),
                   gauge('k8s.pod.memory.working_set', 'By', [dp(t, mem * 2**20 * (0.85 + 0.2 * wave(t, h % 5, 4000, 0, 1)))]),
                   counter('k8s.pod.network.io', 'By', [dp(t, int(net[(pod, d)]), direction=d) for d in ('receive', 'transmit')]),
                   gauge('k8s.container.restarts', '{restart}', [dp(t, 2 if (wl == 'cart' and pod.endswith('1')) else 0)])]
        out.append(rm(r, ms))
    for cid, ws in WORK.items():
        c = CLUSTERS[cid]
        for ns, kind, name, n, cpu, mem in ws:
            if kind != 'deployment': continue
            avail = n - (1 if (cid, name) == PENDING else 0)
            out.append(rm({'ikhnos.cluster.id': cid, 'ikhnos.org.id': 'default', 'k8s.cluster.name': c['name'], 'k8s.namespace.name': ns, 'k8s.deployment.name': name},
                          [gauge('k8s.deployment.desired', '{pod}', [dp(t, n)]), gauge('k8s.deployment.available', '{pod}', [dp(t, avail)])]))
            if ns == 'shop':
                out.append(rm({'ikhnos.cluster.id': cid, 'ikhnos.org.id': 'default'}, [gauge('ikhnos_application_info', '', [dp(t, 1, application='shop', member=f'{name}/{ns}/{cid}', service_name=name, k8s_namespace_name=ns, ikhnos_cluster_id=cid)])]))
    return out

def post_metrics(rms):
    for i in range(0, len(rms), 400):
        req = ExportMetricsServiceRequest(resource_metrics=rms[i:i + 400])
        r = requests.post(PROM, data=req.SerializeToString(), headers={'Content-Type': 'application/x-protobuf'}, timeout=30)
        if r.status_code >= 300: print('prom', r.status_code, r.text[:200], flush=True)

LINES = ['GET /api/items 200 {ms} ms', 'GET /healthz 200 1 ms', 'POST /api/cart 201 {ms} ms', 'cache hit ratio 0.{ms}', 'connection pool: {n} idle', 'GET /api/items/{n} 200 {ms} ms']
ERRS = ['ERROR upstream timeout after 2000 ms', 'error: connection reset by peer', 'WARN slow query {ms} ms']
def logs_at(t):
    rls = []
    for (cid, ns, kind, wl, pod, node, cpu, mem) in PODS:
        c = CLUSTERS[cid]; recs = []
        for k in range(rnd.randint(1, 3)):
            bad = rnd.random() < (0.12 if wl in ('api', 'cart') else 0.02)
            msg = rnd.choice(ERRS if bad else LINES).format(ms=rnd.randint(3, 180), n=rnd.randint(1, 900))
            recs.append(logs_pb2.LogRecord(time_unix_nano=ts(t - rnd.random() * (STEP - 1)), severity_text='ERROR' if bad else 'INFO', body=AnyValue(string_value=msg)))
        recs.sort(key=lambda x: x.time_unix_nano)
        rls.append(logs_pb2.ResourceLogs(resource=Resource(attributes=[kv('ikhnos.cluster.id', cid), kv('ikhnos.org.id', 'default'), kv('k8s.cluster.name', c['name']), kv('k8s.namespace.name', ns), kv('k8s.pod.name', pod), kv('k8s.%s.name' % kind, wl), kv('service.name', wl)]), scope_logs=[logs_pb2.ScopeLogs(log_records=recs)]))
    return rls
def post_logs(rls):
    for i in range(0, len(rls), 60):
        r = requests.post(LOKI, data=ExportLogsServiceRequest(resource_logs=rls[i:i + 60]).SerializeToString(), headers={'Content-Type': 'application/x-protobuf'}, timeout=30)
        if r.status_code >= 300: print('loki', r.status_code, r.text[:200], flush=True)

if __name__ == '__main__':
    def up(u):
        try: return requests.get(u, timeout=5).status_code == 200
        except Exception: return False
    while not (up('http://ikhnos-fusion-loki.ikhnos-system.svc:3100/ready') and up('http://ikhnos-fusion-prometheus.ikhnos-system.svc:9090/-/ready')): time.sleep(3)
    now = time.time()
    t = (now - BACKFILL_MIN * 60) // STEP * STEP
    while t < now:
        post_metrics(metrics_at(t)); post_logs(logs_at(t)); t += STEP
    print('backfilled', flush=True)
    while True:
        t = time.time() // STEP * STEP
        post_metrics(metrics_at(t)); post_logs(logs_at(t))
        time.sleep(STEP)
