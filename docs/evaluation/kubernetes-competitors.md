# Kubernetes in Panel: competitors and direction

Date: 2026-11-05. The Vutty column was checked against the code and against a real kind cluster (both ways in). The competitor columns come from general knowledge and **were not rechecked against their latest versions**; a cell that is uncertain is marked `?`.

## Position

Vutty does not compete with Lens or Rancher on breadth. Its advantage is that Kubernetes sits in the same app as SSH, SFTP, Docker and Panel, and that it can go through SSH or a bastion without installing anything on the server. No competitor does both.

## Quick comparison

| | Vutty | Lens | k9s | Headlamp | Portainer | Rancher |
|---|---|---|---|---|---|---|
| Form | Desktop | Desktop | TUI | Web/Desktop | Web | Web |
| Installed on the server | No | No | No | Yes (in-cluster) | Agent | Yes |
| Through SSH/bastion | ✅ | ❌ | ◐ | ❌ | ❌ | ❌ |
| Only a kubeconfig needed | ✅ | ✅ | ✅ | ✅ | ◐ | ◐ |
| Pods, workloads, services, nodes, events, logs | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Shell, scale, restart, rollback, drain | ✅ | ✅ | ✅ | ✅ | ◐ | ✅ |
| Port-forward | ✅ (host and kubeconfig) | ✅ | ✅ | ✅ | ❌ | ❌ |
| CRDs / any resource | ✅ (read only) | ✅ | ✅ | ✅ | ◐ | ✅ |
| Edit YAML / apply | ◐ (workloads only, host and kubeconfig; no create) | ✅ | ✅ | ✅ | ✅ | ✅ |
| Helm | ◐ (releases are viewed only; no repos or charts) | ✅ | ◐ ? | plugin | ✅ | ✅ |
| Metrics with history | ❌ (current values only, needs metrics-server) | ✅ | ◐ | plugin | ◐ | ✅ |
| Logs of several pods | ✅ (up to 20 pods) | ✅ | ✅ | ◐ | ❌ | ◐ |
| Explains why a pod fails (CrashLoop, OOM, ImagePull…) | ✅ | ? | ? | ? | ? | ? |
| `can-i` before you click | ✅ | ❌ | ❌ | ◐ | ✅ | ✅ |
| Audit log | ✅ | ❌ | ❌ | ❌ | paid | ✅ |
| Security scan (privileged pods, hostPath, secrets in plain, NodePort, Ingress without TLS) | ✅ (read only) | ❌ | ❌ | plugin ? | ◐ | ✅ (paid ?) |
| Ephemeral debug container (`kubectl debug`) for a pod without a shell | ✅ (both ways in) | ◐ ? | ◐ ? | ❌ ? | ❌ | ❌ ? |
| Copy the equivalent kubectl command | ✅ | ◐ | ❌ | ❌ | ❌ | ◐ |
| Files of a pod, nothing installed in the pod | ✅ (read only) | ❌ | ❌ | ❌ | ❌ | ❌ |
| Production marking | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ |
| SSH/SFTP/Docker/VPS in one app | ✅ | ❌ | ❌ | ❌ | ◐ | ❌ |

Changes since the previous version: Vutty's Metrics went from ◐ to ❌ (no history, only current values); the Edit YAML cell says exactly where it works; a row "Explains why a pod fails" was added.

## Fit by kind of user (1–5)

The scores are unchanged, but what is missing has become smaller.

| User | Score | Main gap |
|---|---|---|
| Sysadmin (VPS, k3s/MicroK8s) | 5 | Almost none |
| Developer | 5 | Editing YAML beyond workloads and CRDs. (Logs of several pods and viewing CRDs exist now) |
| New to Kubernetes | 4 | Reasons for CrashLoop, OOM, ImagePull, Evicted, Init and Pending exist; missing: how to fix them, and other failures (PVC, probe, quota: not checked) |
| DevOps / SRE | 3 | apply, changing Helm releases (only viewing now), metrics history, comparing several clusters |
| Platform / cluster admin | 3 | RBAC, quotas. (CRDs can be viewed, read only) |
| Security / audit | 3 | Sending logs elsewhere, viewing RBAC |
| Manager (view only) | 2 | A read-only mode by role (today only buttons locked by `can-i`) |

## Gaps, by priority

| # | Work | Who benefits | Effort | Note |
|---|---|---|---|---|
| 1 | ~~View any resource and CRDs (read only)~~ **done** (Resources tab) | Everyone | Medium | |
| 2 | ~~Logs of several pods by selector/deployment~~ **done** (Logs on a workload and on a Service) | Developers, SRE | Medium | |
| 3 | Hints on failure: **partly done** (the Why column). Left: how to fix, other failures | New users, sysadmins | Low | Only reads the events and status already there |
| 4 | ~~Port-forward for a cluster opened with a kubeconfig~~ **done** (through the API server, tried on kind 1.37) | Developers | Medium | |
| 5 | Metrics with history (a sparkline during the session, or reading Prometheus) | SRE, sysadmins | Medium | Optional |
| 6 | Edit YAML beyond workloads, and for CRDs | DevOps | High | Workloads can be edited (Edit YAML: dry run, diff, audit, refused when the object has changed) |
| 7 | ~~Edit a workload for a cluster opened through a host (kubectl)~~ **done** (Edit YAML, Edit config, Add sidecar through `kubectl replace`, content passed on stdin) | Sysadmins, DevOps | Medium | Not tried on a real host; see Progress |

## To decide

1. Extend Edit YAML to other resources and to CRDs (item 6)?
2. Helm: viewing releases only was chosen (Apps tab). Are writes (rollback, upgrade) needed? Today the terminal is used; a Copy rollback button exists.
3. Main direction: sysadmin/developer (do 3, keep it small) or DevOps/SRE (add 5 and 6, compete with Lens)?

2026-11-05 (after #29): added the **Security** tab, the **Copy kubectl** button, **Debug** (ephemeral container) and port-forward for a cluster opened with a kubeconfig ([Kubernetes panel design](../design/kubernetes-panel.md), section 25); the "Files of a pod" row was added to the table. A competitor cell marked `?` was not checked.

Progress: items 1 and 2 are done; item 3 is partly done; item 4 is done; item 5 is not started; item 7 is done (checked with tests, not tried on a real host); item 6 waits for a decision.

## Done after comparing with Rancher's sidebar

The **Resources** tab (every kind of resource and CRD, read only, with buttons for Namespaces, HPAs, PersistentVolumes and StorageClasses), the **Apps** tab (Helm releases, read only), the logs of all the pods of a Deployment, StatefulSet, DaemonSet, Job and Service, and counts on the Pods, Workloads and Network tabs. Details are in the [Kubernetes panel design](../design/kubernetes-panel.md) (sections 20–23).

Not done, and why: Rancher's Charts and Repositories live outside the cluster (they belong to the cluster Rancher manages, not to the one being viewed); Tools are Rancher's own; counts for Events and Config were dropped because the tab shows something other than the kinds that can be counted.
