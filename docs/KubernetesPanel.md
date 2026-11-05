# Kubernetes in Panel: design

A **Kubernetes** tab in Panel ([Panel roadmap](PanelRoadmap.md)). It runs `kubectl` on a host you already reach over SSH, so it needs nothing on your Mac and nothing installed on the server. The pictures are in [screenshots/k8s/](screenshots/k8s/).

## Principles

These are the Panel principles, kept as they are.

- **Nothing installed on the server.** Only the SSH login the Panel already has. The API server is never contacted directly.
- **Fixed commands only.** Every command is in the [command map](#command-map); names, namespaces and numbers are checked before they go into one. `--context` and `-n` are always passed, so a change of the current context on the host cannot redirect a command.
- **Every change asks first**, locks only its own button while it runs, and is written to the audit log (who, host, context, namespace, what, result; never the output).
- **What the login may not do stays locked**, with the reason in the tooltip (`kubectl auth can-i`).

## Any kind of cluster

The tab works with whatever the host's `kubectl` can reach. How it finds the way in:

| Cluster | What is on the host | What the tab does |
|---------|--------------------|-------------------|
| **kubeadm**, RKE, plain Kubernetes | `kubectl` and `~/.kube/config`, or `/etc/kubernetes/admin.conf` (root only) | Uses `~/.kube/config`; for admin.conf it needs root or passwordless sudo, and says so when it has neither |
| **k3s** | `k3s kubectl`, kubeconfig at `/etc/rancher/k3s/k3s.yaml` (root only) | Finds `k3s kubectl` when there is no `kubectl`; reads the kubeconfig with sudo when the login may |
| **MicroK8s** | `microk8s kubectl` | Same: tries it after `kubectl` and `k3s kubectl` |
| **EKS, GKE, AKS** (managed) | `kubectl` and a kubeconfig whose user runs a plugin (`aws eks get-token`, `gke-gcloud-auth-plugin`, `kubelogin`) | The plugin runs on the host, with the host's cloud credentials. An expired login is reported as "not accepted" with the plugin's own message (screen 3e). There is no control-plane node to open: Shell, Logs, Scale and the rest all go through the API |
| **kind, minikube, k3d** (local) | `kubectl` and a kubeconfig | Works the same; the cluster URL is local to the host |

Things that differ between clusters are read, not assumed: no metrics-server means no CPU and memory columns (they say so); no Ingress controller means an empty Ingress list; a managed cluster that hides its control-plane pods simply shows fewer pods in `kube-system`.

Search order for the command: `kubectl`, `k3s kubectl`, `microk8s kubectl`, then `/usr/local/bin` and `~/.local/bin`. Search order for the kubeconfig: `$KUBECONFIG`, `~/.kube/config`, then (with sudo, when the login may use it) `/etc/rancher/k3s/k3s.yaml` and `/etc/kubernetes/admin.conf`. Only these fixed places are ever tried.

## A cluster from a kubeconfig (no host)

A second way in, for someone who was given a kubeconfig by an administrator and has no SSH login to a host with `kubectl`. Nothing is installed on this computer either: Vutty talks to the API server itself (`internal/kubeapi`, a small client; no `client-go`), so it behaves the same on macOS, Windows and Linux.

- **The same tab.** Every read of the host path is `kubectl get ... -o json`; here the same JSON is asked of the API server (`k8s_direct.go` turns the fixed argument shapes into requests and puts the `kind` on list items, as kubectl does), so the parsers and screens are shared. Every change is a request (scale and pause are merge patches, restart a template annotation then a wait for the rollout, rollback a JSON patch with the template of that revision, drain is cordon then Eviction requests that retry while a disruption budget says no, `auth can-i` is a SelfSubjectAccessReview).
- **Shell** is a terminal tab (`kubeexec:<id>`, like a serial port or Telnet: it shares the buffering, resize, reconnect with Enter and session log of every terminal) that runs `sh -c 'bash || sh'` through the API's exec WebSocket (`v4.channel.k8s.io`; `internal/kubeapi/stream.go`). It is kept for as long as the cluster's tab is open; a production cluster's shell keeps the red frame and is session-logged under the same setting as a prod host.
- **What it cannot do:** Port-forward (it needs a stream per connection on a host with `kubectl`). Its button says so; a later version can add it with the API's `portforward` WebSocket.
- **A login that may not list the cluster** (one Rancher project) has no *All namespaces* and no namespace list. The tab lets the person add namespaces by name; a list of them (`a,b`, only for reads) is asked one namespace after the other and put together, and a namespace that is refused is left out while another answers. The Overview of a namespace says when the nodes (cluster-wide) cannot be read.
- **The kubeconfig is a vault secret** (`SecretKubeconfig`); the record (`Cluster`) holds only the name, group, environment, default context and the approved login programs. It is decrypted into memory for the life of the session and never sent to the interface: the file is read by the backend from the path picked in the native dialog (pasting the text is the fallback).
- **A kubeconfig from someone else is code of a kind.** It is checked when added, not at the first call: a `tokenFile` is refused (it would send a file of this computer to the server the file names), so are the removed `auth-provider` and relative file paths (a vault has no folder to resolve them against). A login plugin (`exec`) runs on this computer: the well-known helpers (`aws`, `gcloud`, `gke-gcloud-auth-plugin`, `az`, `kubelogin`, `kubectl-oidc_login`, ...) run at once, any other program is shown and runs only after the person approves that exact command line, kept with the cluster; replacing the kubeconfig drops the approvals. What the screen sends to approve is checked against the saved kubeconfig, not trusted.
- **Careful with the network.** A redirect is never followed (the token would go with it), the answer of one call is capped, a path with a space or line break is refused, and `insecure-skip-tls-verify` is shown as a warning when the cluster is added.
- **Audit:** the same log lines as for a host (the "host" is `kube:<id>`), never the output.
- **Sync:** the kind *Kubernetes clusters* of Settings → Sync is ticked on its own (`syncClusters`); the kubeconfig of a cluster is a vault secret of kind `kubeconfig` that travels with it, and the *Keys and passwords* switch carries every other secret (`secretsOf` in `sync_what.go`). Unticked means neither sent nor taken.

## 1 · How Vutty reaches the cluster

![How Vutty reaches the cluster](screenshots/k8s/01-flow.png)

## 2 · Journey of a session

![Journey of a session](screenshots/k8s/02-journey.png)

## 3 · Connect: every branch

![Connect, every branch](screenshots/k8s/03-connect-flow.png)

The screens of each step:

| Screen | |
|--------|---|
| 3a · Looking for kubectl | ![](screenshots/k8s/05-connect-detect.png) |
| 3b · Choose a cluster | ![](screenshots/k8s/06-connect-context.png) |
| 3c · What this login may do | ![](screenshots/k8s/07-connect-access.png) |
| 3d · No kubectl on this host | ![](screenshots/k8s/08-no-kubectl.png) |
| 3e · When the connection fails | ![](screenshots/k8s/09-connect-errors.png) |

## 4 · A change, from click to audit

![A change, from click to audit](screenshots/k8s/04-action-flow.png)

## 5 · Cluster overview

![Cluster overview](screenshots/k8s/10-overview.png)

## 6 · Pods

![Pods](screenshots/k8s/11-main.png)

## 7 · Shell

![Choose a container](screenshots/k8s/12-pod-shell.png)

![The terminal tab that opens](screenshots/k8s/14-shell-tab.png)

## 8 · Pod detail

![Pod detail](screenshots/k8s/13-pod-detail.png)

## 9 · Deployments

![Deployments: scale and restart](screenshots/k8s/15-deployments.png)

## 10 · Deployment detail

![Rollout, rollback, autoscaler](screenshots/k8s/16-deploy-detail.png)

## 11 · StatefulSets, DaemonSets, Jobs, CronJobs

![Other workloads](screenshots/k8s/17-other-workloads.png)

## 12 · Services and Ingress

![Services and Ingress](screenshots/k8s/18-services-ingress.png)

## 13 · ConfigMaps, Secrets, volumes

![Config and storage](screenshots/k8s/19-config-storage.png)

## 14 · Port-forward

![Port-forward](screenshots/k8s/20-port-forward.png)

## 15 · Nodes

![Nodes](screenshots/k8s/21-nodes.png)

## 16 · Drain a node

![Drain a node](screenshots/k8s/22-node-drain.png)

## 17 · Events

![Events](screenshots/k8s/23-events.png)

## 18 · YAML (read-only)

![YAML](screenshots/k8s/24-yaml-view.png)

## 19 · Logs of a pod

![Logs of a pod](screenshots/k8s/25-logs-k8s.png)


## 20 · Resources: any kind, CRDs included (read only)

The **Resources** view lists every kind the cluster serves (discovery: `/api`, `/apis`, one request per API group, kept for 5 minutes), picks one, lists its objects and opens one as YAML.

- **Both ways in use the same paths.** A host runs `kubectl get --raw <path>`; a cluster opened with a kubeconfig asks the API server itself. The groups are discovered in one round trip on a host.
- **Only what discovery listed.** The screen sends `{group, version, plural}`; the backend checks each part against a strict pattern and against the list the cluster gave, so there is no path of the screen's own. A kind without the `list` and `get` verbs, a subresource, and the **Secrets of the core group** are never offered or opened here (the Secret screens show names only).
- **Columns.** A CRD's `additionalPrinterColumns` of the version shown (not the wide ones), read with a small JSONPath (`.a.b`, `[n]`, `[*]`, `[?(@.k=="v")]`); Namespaces, PersistentVolumes, StorageClasses and HorizontalPodAutoscalers have columns of their own, and each has a button at the top. Every row ends with its age.
- **Size.** 200 objects for each namespace asked; the screen says when the cluster has more.

## 21 · Apps: Helm releases (read only)

Helm 3 keeps each revision of a release in a Secret `helm.sh/release.v1` (labels `owner=helm`, `name`, `version`, `status`; the value is base64 of gzip of JSON), so nothing is installed anywhere.

- **The list** reads labels only (the latest revision of each release); on a host it asks kubectl for a template of labels, so no Secret data leaves the cluster.
- **A release** is read when it is opened: chart, versions, notes, manifest and the revisions kept. The **Secrets inside the manifest are shown without their data**.
- **Values** often hold passwords: a call of its own, asked first, and audited (that they were shown, never the values).
- **No changes.** A previous revision has *Copy rollback*, which copies `helm rollback <name> <revision> -n <namespace>` for a terminal.
- **Not read:** Helm 2, releases stored in ConfigMaps or SQL, and chart repositories (kept by Helm or Rancher, not by the cluster).

## 22 · Logs of all the pods

*Logs* on a Deployment, StatefulSet, DaemonSet, Job or Service (and in Deployment detail) reads every pod it selects: the pods are found by its selector (`matchLabels`, `matchExpressions`, or the Service's selector), at most 20 pods and 40 containers, five at a time, each with `--timestamps`; the lines are put in the order of their time, the newest `Last N lines` are kept, and each line starts with its pod (`pod` or `pod/container`) in a colour of its own. Pods that have not started are left out; a pod that cannot be read is named above the log. The filter finds a pod by name; *Follow* works as for one pod.

## 23 · Numbers on the tabs

Pods, Workloads and Network show how many there are, from one request for each kind asking for a single item (the API server says how many are left). A tab with a kind that could not be counted shows nothing, not zero. Events and Config show none: what they list is not a count of the kinds.

## Command map

![Command map](screenshots/k8s/26-command-map.png)

## Phases

| Phase | What | Mode |
|-------|------|------|
| **1** | Connect (3a–3e), Overview, Pods, Pod detail, Workloads, Network, Config and storage (Secrets by name only), Resources (any kind), Apps (Helm releases), Nodes, Events, Logs (one pod, or all the pods of a workload), YAML | read only |
| **2** | Shell, Port-forward, Scale, Restart, Pause and resume, Roll back, Delete pod, CronJob run now and suspend, Cordon and uncordon, Drain | each asks first, audited |
| **3** | Reveal one Secret key (30 s on screen, audited) | asks first, audited |

Not planned: `apply` of new objects, `delete` of anything but a pod or a Job, and any change to a Helm release (install, upgrade, rollback, uninstall): use the terminal. The Apps view copies the `helm rollback` command instead.
