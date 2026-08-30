#!/usr/bin/env python3
"""Prints the live Cluster -> Node -> Pod -> Container hierarchy from the API.

Used to verify that the backend reports the same health picture as kubectl,
including the cases a phase-only view gets wrong: a pod in CrashLoopBackOff
still reports phase Running, and a pod that is Running but failing its
readiness probe receives no traffic.

Usage: scripts/hierarchy-check.py [backend-url]
"""
import json
import sys
import urllib.request

BACKEND = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8090"
KEYCLOAK = "http://localhost:8080"


def post(url, data, token=None, form=False):
    if form:
        body = "&".join(f"{k}={v}" for k, v in data.items()).encode()
        headers = {"Content-Type": "application/x-www-form-urlencoded"}
    else:
        body = json.dumps(data).encode()
        headers = {"Content-Type": "application/json", "Connect-Protocol-Version": "1"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=body, headers=headers, method="POST")
    with urllib.request.urlopen(req, timeout=45) as resp:
        return json.load(resp)


token = post(
    f"{KEYCLOAK}/realms/idp/protocol/openid-connect/token",
    {"grant_type": "password", "client_id": "idp-frontend", "username": "admin", "password": "admin"},
    form=True,
)["access_token"]


def rpc(method, body=None):
    return post(f"{BACKEND}/idp.v1.{method}", body or {}, token=token)


nodes = rpc("ClusterService/ListNodes").get("nodes", [])
pods = rpc("ClusterService/ListPods", {"namespace": ""}).get("pods", [])

by_node = {}
for p in pods:
    by_node.setdefault(p.get("node") or "<unscheduled>", []).append(p)

problems = []

for n in nodes:
    flags = []
    if n.get("unschedulable"):
        flags.append("SchedulingDisabled")
    flags += n.get("pressureConditions", [])
    print(
        f"CLUSTER NODE  {n['name']}  [{n['status']}{' ' + ','.join(flags) if flags else ''}]  "
        f"{n.get('kubeletVersion','')}"
    )
    print(
        f"  capacity   pods {n.get('podCount',0)}/{n.get('podCapacity',0)} ({n.get('podsPercent',0)}%)  "
        f"cpu {n.get('cpuRequests','0')}/{n.get('cpuAllocatable','0')} ({n.get('cpuRequestsPercent',0)}%)  "
        f"mem {n.get('memoryRequests','0')}/{n.get('memoryAllocatable','0')} ({n.get('memoryRequestsPercent',0)}%)"
    )
    if n.get("statusMessage"):
        print(f"  ! {n['statusMessage']}")
    if n.get("podsPercent", 0) >= 90:
        problems.append(f"node {n['name']} is at {n['podsPercent']}% of its pod capacity")

    for p in sorted(by_node.get(n["name"], []), key=lambda x: (x["namespace"], x["name"])):
        ready = "ready" if p.get("ready") else "NOT-READY"
        rst = p.get("restartCount", 0)
        print(
            f"    POD  {p['namespace']}/{p['name']}  [{p.get('status','')}] {ready}  "
            f"restarts={rst}  qos={p.get('qosClass','')}  phase={p.get('phase','')}"
        )
        if p.get("schedulingMessage"):
            print(f"      ! unschedulable: {p['schedulingMessage']}")
            problems.append(f"pod {p['namespace']}/{p['name']} cannot be scheduled")
        if p.get("reason") or p.get("message"):
            print(f"      ! {p.get('reason','')} {p.get('message','')}".rstrip())

        for c in p.get("containers", []):
            probes = "".join(
                x for x in (
                    "L" if c.get("hasLivenessProbe") else "",
                    "R" if c.get("hasReadinessProbe") else "",
                    "S" if c.get("hasStartupProbe") else "",
                )
            ) or "-"
            res = (
                f"req {c.get('cpuRequest') or '-'}/{c.get('memoryRequest') or '-'}  "
                f"lim {c.get('cpuLimit') or '-'}/{c.get('memoryLimit') or '-'}"
            )
            print(
                f"      CONTAINER  {c['name']}  [{c.get('state','')}"
                f"{'/' + c['reason'] if c.get('reason') else ''}]  "
                f"{'ready' if c.get('ready') else 'not-ready'}  restarts={c.get('restartCount',0)}  "
                f"probes={probes}  {res}"
            )
            if c.get("lastTerminationReason"):
                print(
                    f"        last exit: {c['lastTerminationReason']} "
                    f"(code {c.get('lastExitCode', 0)})"
                )
            if not c.get("cpuRequest") or not c.get("memoryRequest"):
                problems.append(
                    f"container {p['namespace']}/{p['name']}/{c['name']} has no resource requests"
                )
            if c.get("reason") in ("CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "OOMKilled"):
                problems.append(
                    f"container {p['namespace']}/{p['name']}/{c['name']} is {c['reason']}"
                )

unscheduled = by_node.get("<unscheduled>", [])
if unscheduled:
    print(f"\nUNSCHEDULED PODS ({len(unscheduled)})")
    for p in unscheduled:
        print(f"    POD  {p['namespace']}/{p['name']}  [{p.get('status','')}]")
        if p.get("schedulingMessage"):
            print(f"      ! {p['schedulingMessage']}")

print(f"\n{len(nodes)} node(s), {len(pods)} pod(s)")
if problems:
    print(f"\n{len(problems)} problem(s) detected:")
    seen = set()
    for pr in problems:
        if pr not in seen:
            seen.add(pr)
            print(f"  - {pr}")
