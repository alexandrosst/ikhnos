"""Runs topology_example.py against a small stand-in for the FUSION API, so the example cannot drift from the shapes it reads.

    python3 -m unittest discover examples/topology
"""
import http.server
import json
import os
import subprocess
import sys
import threading
import unittest
import urllib.parse

HERE = os.path.dirname(os.path.abspath(__file__))
TOKEN = "cnf_test"

EDGE = {"id": "cl-edge", "name": "athens-edge", "tier": "edge"}
CLOUD = {"id": "cl-cloud", "name": "berlin", "tier": "cloud"}
CART = {"key": "cart/shop/cl-edge", "name": "cart", "placement": {"cluster": EDGE, "nodes": [{"id": "n1", "name": "edge-a", "pods": 2}, {"id": "n2", "name": "edge-b", "pods": 1}]}}
WEB = {"key": "web/shop/cl-edge", "name": "web", "placement": {"cluster": EDGE, "nodes": [{"id": "n1", "name": "edge-a"}]}}
PAY = {"key": "pay/shop/cl-cloud", "name": "pay", "placement": {"cluster": CLOUD, "nodes": [{"id": "n3", "name": "cloud-a", "pods": 1}]}}
GHOST = {"key": "ghost/shop/cl-cloud", "name": "ghost"}  # a service the topology does not know: no placement

TOPOLOGY = {"application": {"id": "app-1", "name": "Shop"}, "health": {"status": "healthy"}, "services": [CART, WEB, PAY, GHOST]}
NODELINK = {
    "nodes": [
        {"id": "service:" + s["key"], "type": "service", "name": s["name"], "placement": s.get("placement")} for s in (CART, WEB, PAY, GHOST)
    ] + [{"id": "external:x-1", "type": "external", "name": "GitHub"}],
    "links": [
        {"source": "service:" + CART["key"], "target": "service:" + PAY["key"]},      # across clusters
        {"source": "service:" + WEB["key"], "target": "service:" + CART["key"]},      # inside one
        {"source": "service:" + CART["key"], "target": "external:x-1"},               # to an address: no cluster to compare
    ],
}

seen = []


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        url = urllib.parse.urlparse(self.path)
        query = urllib.parse.parse_qs(url.query)
        seen.append((url.path, {k: v[0] for k, v in query.items()}))
        if self.headers.get("Authorization") != f"Bearer {TOKEN}":
            self.send_response(401)
            self.end_headers()
            return
        if url.path == "/api/v1/fusion/applications":
            answer = {"applications": [{"id": "app-1", "name": "Shop"}]}
        elif url.path == "/api/v1/fusion/applications/Shop/topology" or url.path == "/api/v1/fusion/applications/app-1/topology":
            answer = NODELINK if query.get("format") == ["nodelink"] else TOPOLOGY
        else:
            self.send_response(404)
            self.end_headers()
            return
        body = json.dumps(answer).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class TopologyExample(unittest.TestCase):
    def run_example(self, *args):
        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            env = {**os.environ, "IKHNOS_ADDR": f"http://127.0.0.1:{server.server_port}", "FUSION_TOKEN": TOKEN}
            return subprocess.run([sys.executable, os.path.join(HERE, "topology_example.py"), *args], env=env, capture_output=True, text=True, timeout=30)
        finally:
            server.shutdown()

    def test_prints_where_each_service_runs_and_which_calls_cross_clusters(self):
        seen.clear()
        run = self.run_example("Shop")
        self.assertEqual(run.returncode, 0, run.stderr)
        for expected in (
            "Shop healthy 4 services",
            "cart: athens-edge (edge) on edge-a x2, edge-b x1",
            "pay: berlin (cloud) on cloud-a x1",
            "ghost: not in the topology",
            "cl-edge/edge-a runs cart, web",
            "service:cart/shop/cl-edge -> service:pay/shop/cl-cloud crosses from cl-edge to cl-cloud",
        ):
            self.assertIn(expected, run.stdout)
        self.assertNotIn("web/shop/cl-edge -> service:cart", run.stdout)  # inside one cluster: not a crossing
        self.assertNotIn("GitHub", run.stdout)
        self.assertTrue(all(q.get("placement") == "true" for _, q in seen), seen)  # it asks for the placement every time

    def test_takes_the_first_application_when_none_is_named(self):
        seen.clear()
        run = self.run_example()
        self.assertEqual(run.returncode, 0, run.stderr)
        self.assertIn("Shop healthy", run.stdout)
        self.assertIn("/api/v1/fusion/applications/app-1/topology", [p for p, _ in seen])


if __name__ == "__main__":
    unittest.main()
