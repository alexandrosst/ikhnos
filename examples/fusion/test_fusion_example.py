"""Runs fusion_example.py against a small stand-in for the FUSION API, so the example cannot drift from the shapes it reads.

    python3 -m unittest discover examples/fusion
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

ANSWERS = {
    "/api/v1/fusion/status": {"access": {"name": "ci", "signals": ["metrics", "traces"], "namespaces": []}},
    "/api/v1/fusion/traces": {
        "results": [
            {"id": "a", "status": 200, "trace": {"traceId": "a", "durationMs": 12.3, "errorCount": 1, "spans": [{"service": "checkout", "logs": [{"line": "card declined"}]}]}},
            {"id": "b", "status": 404, "error": "not found"},
        ]
    },
    "/api/v1/fusion/metrics/range": {"series": [{"name": "cpu", "labels": {"pod": "p-1"}, "last": 0.2}]},
    "/api/v1/fusion/links": {"pairs": [{"a": "cl-eu-1", "b": "cl-eu-2", "rttSeconds": 0.012, "lossAToB": 0.02}]},
}


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path
        if self.headers.get("Authorization") != f"Bearer {TOKEN}":
            self.send_response(401)
            self.end_headers()
            return
        body = json.dumps(ANSWERS[path]).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class FusionExample(unittest.TestCase):
    def test_reads_every_part_of_the_answers_it_prints(self):
        server = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=server.serve_forever, daemon=True).start()
        try:
            env = {**os.environ, "IKHNOS_ADDR": f"http://127.0.0.1:{server.server_port}", "FUSION_TOKEN": TOKEN}
            run = subprocess.run([sys.executable, os.path.join(HERE, "fusion_example.py")], env=env, capture_output=True, text=True, timeout=30)
        finally:
            server.shutdown()
        self.assertEqual(run.returncode, 0, run.stderr)
        for expected in ("ci ['metrics', 'traces'] all namespaces", "checkout card declined", "b 404 not found", "cpu {'pod': 'p-1'} last 0.2", "cl-eu-1 cl-eu-2 0.012 0.02"):
            self.assertIn(expected, run.stdout)


if __name__ == "__main__":
    unittest.main()
