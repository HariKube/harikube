# HariKube

## What is this?

Normally, Kubernetes uses a database called ETCD. [Kine](https://github.com/k3s-io/kine) (the origin of this project) is a tool that allows Kubernetes to use other databases (like SQLite or PostgreSQL) instead.

This specific version of Kine is unique because it handles filtering and garbage-collection directly at the database level, which can make your cluster much faster and more efficient.

## Why this fork exists?

Both ETCD and Kine are limited by Kubernetes API server itself and how it filters data. API server manages an O(n) cache in memory, and filters data at client side, because both ETCD and Kine are lacking on data filtering. The only real option is vertical scaling of all (API, ETCD, Kine). An average cluster dies at 50-100k records. Of course, you can add more ram, more iops, but these are just postponing the problem.

By changing a few lines of Kubernetes and a few lines of Kine, this project is able to send filtering to the database level. With these changes it is able to disable watch cache in Kubernetes API, and consumes O(1) memory during operation.

Here are some benchmark results on Ultra 7 165H 18 Core 4G, single VM ran everything including the k6 benchmark itself. 120 vus, each vu created a custom resource (6 different type) and read it back via label selector:

- Vanilla Kubernetes with 3 node ETCD cluster:

```
checks_total.......: 51236 24.976013/s
checks_succeeded...: 100.00% 51236 out of 51236
checks_failed......: 0.00% 0 out of 51236

HTTP
http_req_duration..............: avg=799.54ms min=3.87ms med=82.39ms max=4.17s p(90)=2.47s p(95)=2.82s
  { expected_response:true }...: avg=799.54ms min=3.87ms med=82.39ms max=4.17s p(90)=2.47s p(95)=2.82s
http_req_failed................: 0.00% 0 out of 51236
http_reqs......................: 51236 24.976013/s

time="2026-02-14T19:07:26Z" level=error msg="test run was aborted because k6 received a 'interrupt' signal" make: *** [Makefile:589: k6s-start] Error 105

OOM Killed, thanks API server
```

- HariKube OSS with Postgres:

```
checks_total.......: 101772  28.188433/s
checks_succeeded...: 100.00% 101772 out of 101772
checks_failed......: 0.00%   0 out of 101772

HTTP
http_req_duration..............: avg=708.33ms min=6.4ms    med=300.67ms max=6.2s  p(90)=1.99s p(95)=2.48s
  { expected_response:true }...: avg=708.33ms min=6.4ms    med=300.67ms max=6.2s  p(90)=1.99s p(95)=2.48s
http_req_failed................: 0.00%  0 out of 101772
http_reqs......................: 101772 28.188433/s
```

## The numbers are talking for themselves

| Metric | HariKube OSS | Vanilla K8s |
| - | - | - |
|Throughput | 28 req/s ✅ | 25 req/s ❌ |
|Success Rate | 100% ✅| 100% (OOM) ❌ |
|Latency average | 708ms ✅ | 799ms ❌  |
|Latency p95 | 2480ms ✅ | 2820ms ❌ |
|Latency p90 | 1990ms ✅ | 2470ms ❌ |
|Test Duration | 60m ✅ | ~34m (OOM) ❌ |
|Stability | Completed ✅  | KILLED ❌ |
|Objects Handled | 50k ✅ | ~26k (OOM) ❌  |

### HariKube on steroids with 6 Postgres

```
checks_total.......: 429180  119.106435/s
checks_succeeded...: 100.00% 429180 out of 429180
checks_failed......: 0.00%   0 out of 429180

HTTP
http_req_duration..............: avg=167.17ms min=7.75ms   med=71.06ms max=3.71s  p(90)=398ms p(95)=543.76ms
  { expected_response:true }...: avg=167.17ms min=7.75ms   med=71.06ms max=3.71s  p(90)=398ms p(95)=543.76ms
http_req_failed................: 0.00%  0 out of 429180
http_reqs......................: 429180 119.106435/s
```

| Metric | HariKube AE | Vanilla K8s | Gain  |
| - | - | - | - |
|Throughput | 119 req/s ✅ | 25 req/s ❌ | 4.8×  |
|Success Rate | 100% ✅  | 100% (then OOM) ❌ | not comparable  |
|Latency average | 167ms ✅ | 799ms ❌  | 4.8× |
|Latency p95 | 543ms ✅  | 2820ms ❌ | 5.2×  |
|Latency p90 | 398ms ✅ | 2470ms ❌ | 6.2×  |
|Test Duration | 60m ✅ | ~34m (OOM) ❌ | not comparable  |
|Stability | Completed ✅ | KILLED ❌ | not comparable  |
|Objects Handled | 200k+ ✅  | ~26k (crashed) ❌  | 8×    |

Open-Source edition is designed to interface with a single backend database instance at a time, which can become a performance bottleneck as your cluster grows. To address this, our advanced editions introduce various data routing capabilities. This allows you to distribute workloads across multiple database backends simultaneously, ensuring horizontal scalability for even the most demanding environments. Check out which [edition](https://harikube.info/editions/) fit's to your use-case.

## Installation: The vCluster Method

The easiest way to use this setup is inside a vCluster (a virtual cluster running inside your real cluster). This keeps everything bundled together.

- Step A: Bring your own cluster

- Step B: Deploy the vCluster

Run these commands to create your virtual cluster using a pre-configured SQLite setup:

```bash
# This creates the virtual cluster resources
# Edit .spec.template.spec.containers.harikube.args to configure database
kubectl apply -f https://github.com/HariKube/harikube/releases/download/release-v0.14.11/vcluster-harikube-sqlite-release-v0.14.11.yaml

# Wait for readiness
kubectl wait -n harikube --for=jsonpath='{.status.readyReplicas}'=1 statefulset/harikube --timeout=5m

# This connects your local terminal to the new virtual cluster
vcluster connect harikube

# This creates and admission policy to disable metadata caching
kubectl apply -f https://github.com/HariKube/harikube/releases/download/release-v0.14.11/skip-controller-manager-metadata-caching.yaml
```

> 🔓 vCluster simplifies the operational workflow by automatically updating your local environment. For more details how to disable this behaviour, or how to get config by service account for example please wisit the official docs` [Access and expose vCluster](https://www.vcluster.com/docs/vcluster/manage/accessing-vcluster) section.

> 🔓 For service access from host, the vCluster setup keeps things simple: Create your ServiceAccount, create a secret annotated with `kubernetes.io/service-account.name` (example below), and vCluster will sync the secret to the host cluster.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: remote-your-service-account-name
  annotations:
    kubernetes.io/service-account.name: "your-service-account-name"
type: kubernetes.io/service-account-token
```

On the host cluster, you can fetch the connection details.

```bash
KUBE_API_URL=harikube.harikube.svc.cluster.local
TOKEN=$(kubectl get secret -n harikube remote-your-service-account-name-x-default-x-harikube -o jsonpath='{.data.token}' | base64 -d)
CA_CERT=$(kubectl get secret -n harikube remote-your-service-account-name-x-default-x-harikube -o jsonpath='{.data.ca\.crt}' | base64 -d)
```

- Step C: Enjoy

## Important Requirement

To use these features to their full potential, you cannot use "standard" Kubernetes. You must use the patched images provided by us These patches allow the Kubernetes API to understand the special storage instructions HariKube is waiting for.

- [Kubernetes Patches](https://github.com/HariKube/kubernetes-patches)
- [Patched Images](https://quay.io/repository/harikube/kubernetes?tab=tags&tag=latest)

## 🙏 Share Feedback and Report Issues

Your feedback is invaluable in helping us improve this operator. If you encounter any issues, have a suggestion for a new feature, or simply want to share your experience, we want to hear from you!

- Report Bugs: If you find a bug, please open a [GitHub Issue](https://github.com/HariKube/harikube/issues). Include as much detail as possible, such as steps to reproduce the bug, expected behavior, and your environment (e.g., Kubernetes version).
- Request a Feature: If you have an idea for a new feature, open a [GitHub Issue](https://github.com/HariKube/harikube/issues) and use the `enhancement` label. Describe the use case and how the new feature would benefit the community.
- Ask a Question: For general questions or discussions, please use the [GitHub Discussions](https://github.com/HariKube/harikube/discussions).
