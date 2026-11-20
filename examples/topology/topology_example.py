"""Read the topology of an application from Python: where each of its services runs, who calls whom, and which calls cross clusters.

    IKHNOS_ADDR=https://ikhnos.example FUSION_TOKEN=cnf_... python3 topology_example.py Shop

The argument is the application's name or id (default: the first application the token sees). The token is made on the FUSION page,
Access tab. Needs `requests` (pip install requests). The API is described in docs-site/docs/architecture/fusion-api.md;
test_topology_example.py runs this script against a stand-in server.
"""
import os
import sys
from collections import defaultdict

import requests

ADDR = os.environ["IKHNOS_ADDR"]      # for example https://ikhnos.example
TOKEN = os.environ["FUSION_TOKEN"]    # cnf_..., made on FUSION > Access

s = requests.Session()
s.headers["Authorization"] = f"Bearer {TOKEN}"
# s.verify = "ikhnos.crt"             # the server's certificate, if it is self-signed: openssl s_client -connect HOST:8080 </dev/null | openssl x509 > ikhnos.crt


def get(path, **params):
    r = s.get(f"{ADDR}/api/v1/fusion/{path}", params=params, timeout=40)
    r.raise_for_status()              # 401 bad token, 403 outside its scope, 404 no such application, 429 slow down
    return r.json()


if len(sys.argv) > 1:
    app = sys.argv[1]
else:
    listed = get("applications")["applications"]
    if not listed:
        sys.exit("this token sees no application; pass one's name or id")
    app = listed[0]["id"]

# The application's services, each with the cluster it runs in and the nodes that hold its pods
topo = get(f"applications/{app}/topology", placement="true")
print(topo["application"]["name"], topo["health"]["status"], f'{len(topo["services"])} services')
for svc in topo["services"]:
    where = svc.get("placement")      # absent for a service the topology does not know
    if where is None:
        print(f'  {svc["name"]}: not in the topology')
        continue
    nodes = ", ".join(f'{n["name"]}' + (f' x{n["pods"]}' if n.get("pods") else "") for n in where["nodes"]) or "no known node"
    cluster = where["cluster"]
    print(f'  {svc["name"]}: {cluster.get("name", cluster["id"])} ({cluster.get("tier", "?")}) on {nodes}')

# The other way round: which services share a node
by_node = defaultdict(list)
for svc in topo["services"]:
    for n in (svc.get("placement") or {}).get("nodes", []):
        by_node[(svc["placement"]["cluster"]["id"], n["name"])].append(svc["name"])
for (cluster, node), names in sorted(by_node.items()):
    if len(names) > 1:
        print(f"{cluster}/{node} runs {', '.join(sorted(names))}")

# The same answer as a graph: nodes are services and addresses, links go from the client to the server. The service nodes carry the
# placement too. With NetworkX: nx.node_link_graph(graph, edges="links")
graph = get(f"applications/{app}/topology", placement="true", format="nodelink")
home = {n["id"]: (n.get("placement") or {}).get("cluster", {}).get("id") for n in graph["nodes"] if n["type"] == "service"}
for link in graph["links"]:
    a, b = home.get(link["source"]), home.get(link["target"])
    if a and b and a != b:
        print(f'{link["source"]} -> {link["target"]} crosses from {a} to {b}')
