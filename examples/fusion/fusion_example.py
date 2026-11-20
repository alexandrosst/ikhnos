"""Read FUSION from Python: what the token may read, the failing traces of a service with their logs, a metric over time, and the cluster links.

    IKHNOS_ADDR=https://ikhnos.example FUSION_TOKEN=cnf_... python3 fusion_example.py

The token is made on the FUSION page, Access tab. Needs `requests` (pip install requests). The API is described in
docs-site/docs/architecture/fusion-api.md; test_fusion_example.py runs this script against a stand-in server.
"""
import os
import requests

ADDR = os.environ["IKHNOS_ADDR"]      # for example https://ikhnos.example
TOKEN = os.environ["FUSION_TOKEN"]    # cnf_..., made on FUSION > Access

s = requests.Session()
s.headers["Authorization"] = f"Bearer {TOKEN}"
# s.verify = "ikhnos.crt"             # the server's certificate, if it is self-signed: openssl s_client -connect HOST:8080 </dev/null | openssl x509 > ikhnos.crt


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
