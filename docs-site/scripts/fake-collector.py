#!/usr/bin/env python3
# Stand-in for the cluster collector's own metrics port: Prometheus text counters that grow, as a collector that is sending would show.
import http.server, time, sys
T0 = time.time()
def body():
    t = time.time() - T0
    m = int(t * 140); l = int(t * 95)
    out = []
    for sig, name, n in (("metrics", "metric_points", m), ("logs", "log_records", l)):
        out.append(f'otelcol_exporter_sent_{name}_total{{exporter="otlp",service_instance_id="c1"}} {n}')
        out.append(f'otelcol_exporter_send_failed_{name}_total{{exporter="otlp",service_instance_id="c1"}} 0')
        out.append(f'otelcol_exporter_queue_size{{exporter="otlp",data_type="{sig}"}} 0')
        out.append(f'otelcol_exporter_queue_capacity{{exporter="otlp",data_type="{sig}"}} 1000')
        out.append(f'otelcol_receiver_accepted_{name}_total{{receiver="otlp",transport="grpc"}} {n}')
        out.append(f'otelcol_receiver_refused_{name}_total{{receiver="otlp",transport="grpc"}} 0')
    for r, name, k in (("hostmetrics","metric_points",40),("kubelet_stats","metric_points",60),("k8s_cluster","metric_points",25),("filelog/containers","log_records",80)):
        out.append(f'otelcol_receiver_accepted_{name}_total{{receiver="{r}",transport=""}} {int(t*k)}')
        out.append(f'otelcol_receiver_refused_{name}_total{{receiver="{r}",transport=""}} 0')
    out.append(f'otelcol_process_uptime_seconds_total {t:.1f}')
    return ("\n".join(out) + "\n").encode()
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        b = body(); self.send_response(200); self.send_header("Content-Type", "text/plain; version=0.0.4"); self.send_header("Content-Length", str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self, *a): pass
http.server.ThreadingHTTPServer(("127.0.0.1", 8899), H).serve_forever()
