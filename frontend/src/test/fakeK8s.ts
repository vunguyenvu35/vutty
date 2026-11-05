import type {Backend} from '../lib/backend'
import type {K8sContainerEdit, K8sEditable, K8sEditPreview, K8sSidecar, K8sWorkloadSpec, K8sAccess, K8sCheck, K8sConfig, K8sContexts, K8sDeployment, K8sDetect, K8sDrainPlan, K8sEvent, K8sAPIResource, K8sObjList, K8sMultiLogs, RemoteEntry, K8sHelmRelease, K8sHelmDetail, K8sSecurity, K8sDebug, K8sForward, K8sNetwork, K8sNode, K8sOverview, K8sPod, K8sPodDetail, K8sWorkload} from '../lib/types'

/** What the fake cluster answers: the data of the design boards (docs/screenshots/k8s), so tests and demos read like them. */
export interface K8sFake {
  detect: K8sDetect
  contexts: K8sContexts
  /** what the check of a context answers (by name; 'default' for any other) */
  checks: Record<string, K8sCheck>
  /** right id -> allowed; an id that is missing is allowed */
  rights: Record<string, boolean>
  namespaces: string[]
  overview: K8sOverview
  nodes: K8sNode[]
  pods: K8sPod[]
  workloads: K8sWorkload[]
  deployment: K8sDeployment
  network: K8sNetwork
  config: K8sConfig
  configMaps: Record<string, Record<string, string>>
  secretValues: Record<string, string>
  events: K8sEvent[]
  logs: string
  yaml: string
  /** the kinds the cluster serves, the objects of each by plural, and the YAML that any object answers with (k8sResources, k8sObjects, k8sObjectYAML) */
  resources: K8sAPIResource[]
  objects: Record<string, K8sObjList>
  objectYaml: string
  /** what the files of a pod are, by folder (podFilesList) */
  podFiles: Record<string, RemoteEntry[]>
  /** Helm releases (k8sHelmReleases), the detail of each by name (k8sHelmRelease answers with the revision asked for), and the values k8sHelmValues answers with */
  /** what k8sSecurity answers (a namespace gets the findings of that namespace) */
  security: K8sSecurity
  helm: K8sHelmRelease[]
  helmDetail: Record<string, K8sHelmDetail>
  helmValues: string
  drain: K8sDrainPlan
  forwards: K8sForward[]
  /** how the containers of a workload are set up (k8sWorkloadSpec; any workload answers with it) */
  spec: K8sWorkloadSpec
  /** what k8sEditable answers, and what k8sSaveYAML says it wrote */
  editable: K8sEditable
  savedPath: string
  /** what k8sEditPreview answers; `previewError` makes it fail with this sentence instead */
  preview: K8sEditPreview
  previewError: string
  /** what k8sDebugStart answers */
  debug: K8sDebug
  /** the line k8sShellCommand answers */
  shell: string
  /** a change fails with this message */
  fail: string
  /** a change waits for this promise first (a test releases it), to look at the button that waits */
  hold: Promise<void> | null
}

const ctr = (name: string, image: string, o: Partial<K8sPod['containers'][0]> = {}) => ({name, image, init: false, ready: true, running: true, state: 'Running', restarts: 0, resources: '250m → 1 · 256Mi → 512Mi', probes: 'liveness, readiness', ...o})
const pod = (name: string, namespace: string, o: Partial<K8sPod> = {}): K8sPod => ({name, namespace, status: 'Running', tone: 'ok', ready: '1/1', restarts: 0, node: 'worker-1', age_sec: 3 * 86400, why: '', owner: '', containers: [ctr(name.split('-')[0], 'registry/app:1')], ...o})
const wl = (kind: K8sWorkload['kind'], name: string, namespace: string, o: Partial<K8sWorkload> = {}): K8sWorkload => ({kind, name, namespace, ready: '1/1', desired: 1, available: 1, up_to_date: 1, image: '', age_sec: 3 * 86400, tone: 'ok', note: '', schedule: '', last_run_sec: null, suspended: false, result: '', volumes: '', hpa: false, ...o})

export function makeK8s(): K8sFake {
  return {
    detect: {found: true, cmd: 'kubectl', path: '/usr/local/bin/kubectl', client_version: 'v1.30.4', tried: []},
    contexts: {kubeconfig: '', sudo: false, kind: '', raw: '', contexts: [
      {name: 'prod-cluster', cluster: 'prod', server: 'https://10.0.0.11:6443', user: 'admin', current: false},
      {name: 'staging-cluster', cluster: 'staging', server: 'https://10.0.1.11:6443', user: 'deploy', current: false},
      {name: 'kind-dev', cluster: 'kind', server: 'https://127.0.0.1:41233', user: 'kind-dev', current: true},
    ]},
    checks: {default: {ok: true, kind: '', raw: '', server: 'https://10.0.0.11:6443', server_version: 'v1.30.2'}},
    rights: {},
    namespaces: ['shop', 'data', 'ingress-nginx', 'monitoring', 'kube-system'],
    overview: {
      server_version: 'v1.30.2', nodes: {total: 3, ready: 3, cordoned: 1}, pods: {total: 44, running: 41, pending: 2, failing: 1},
      cpu_requested_pct: 58, mem_requested_pct: 81, cpu_cores: 12, mem_gib: 46,
      capacity: {known: true, partial: false, pods_used: 44, pods_allocatable: 330, cpu_reserved: 6.96, cpu_allocatable: 12, mem_reserved_gib: 37.26, mem_allocatable_gib: 46, cpu_used: 3.1, mem_used_gib: 21.4},
      namespaces: [{name: 'shop', pods: 18, not_ready: 3, restarts: 16}, {name: 'data', pods: 7, not_ready: 1, restarts: 2}, {name: 'ingress-nginx', pods: 3, not_ready: 0, restarts: 0}, {name: 'monitoring', pods: 9, not_ready: 0, restarts: 1}, {name: 'kube-system', pods: 7, not_ready: 0, restarts: 0}],
      attention: [
        {severity: 'bad', kind: 'pod', namespace: 'shop', name: 'api-7d9f6c-x2kq8', text: 'Pod api-7d9f6c-x2kq8 in shop: CrashLoopBackOff, 14 restarts'},
        {severity: 'bad', kind: 'node', namespace: '', name: 'worker-2', text: 'Node worker-2 is NotReady for 18 minutes'},
        {severity: 'warn', kind: 'deployment', namespace: 'shop', name: 'api', text: 'Deployment api has 1 of 3 replicas ready'},
        {severity: 'warn', kind: 'ingress', namespace: 'shop', name: 'shop.example.com', text: 'Certificate of Ingress shop.example.com expires in 9 days'},
        {severity: 'warn', kind: 'pvc', namespace: 'data', name: 'data-db-0', text: 'PVC data-db-0 is 91% full'},
      ],
    },
    nodes: [
      {name: 'master-1', status: 'Ready', roles: 'control-plane', version: 'v1.30.2', ip: '10.0.0.11', unschedulable: false, cpu_pct: 12, mem_pct: 41, pods: 14, pods_max: 110, age_sec: 120 * 86400},
      {name: 'worker-1', status: 'Ready', roles: 'worker', version: 'v1.30.2', ip: '10.0.0.21', unschedulable: false, cpu_pct: 63, mem_pct: 86, pods: 19, pods_max: 110, age_sec: 120 * 86400},
      {name: 'worker-2', status: 'NotReady', roles: 'worker', version: 'v1.30.2', ip: '10.0.0.22', unschedulable: true, cpu_pct: null, mem_pct: null, pods: 8, pods_max: 110, age_sec: 96 * 86400},
    ],
    pods: [
      pod('api-7d9f6c-x2kq8', 'shop', {status: 'CrashLoopBackOff', tone: 'bad', ready: '0/1', restarts: 14, node: 'worker-2', why: 'Back-off restarting failed container (exit code 1)', owner: 'ReplicaSet api-7d9f6c ← Deployment api',
        containers: [ctr('api', 'registry/api:1.8', {ready: false, running: false, state: 'Waiting: CrashLoopBackOff', restarts: 14})]}),
      pod('worker-5c8b4-p9qzt', 'shop', {status: 'Pending', tone: 'warn', ready: '0/1', node: '', age_sec: 120, why: '0/3 nodes are available: insufficient memory', owner: 'ReplicaSet worker-5c8b4 ← Deployment worker', containers: [ctr('worker', 'registry/worker:2.0', {ready: false, running: false, state: 'Waiting: PodInitializing'})]}),
      pod('web-6b7d9-m4zwn', 'shop', {ready: '2/2', owner: 'ReplicaSet web-6b7d9 ← Deployment web', containers: [
        ctr('init-config', 'busybox:1.36', {init: true, running: false, state: 'Completed'}), ctr('web', 'registry/web:4.2'), ctr('nginx-sidecar', 'nginx:1.27')]}),
      pod('web-6b7d9-q8t2c', 'shop', {ready: '2/2', node: 'master-1', owner: 'ReplicaSet web-6b7d9 ← Deployment web', containers: [ctr('web', 'registry/web:4.2'), ctr('nginx-sidecar', 'nginx:1.27')]}),
      pod('redis-0', 'shop', {age_sec: 17 * 86400, owner: 'StatefulSet redis', containers: [ctr('redis', 'redis:7')]}),
      pod('cron-sync-28811-6lvdm', 'data', {status: 'ImagePullBackOff', tone: 'warn', ready: '0/1', node: 'worker-1', age_sec: 660, owner: 'Job cron-sync-28811', containers: [ctr('sync', 'registry/sync:3.1', {ready: false, running: false, state: 'Waiting: ImagePullBackOff'})]}),
    ],
    workloads: [
      wl('Deployment', 'api', 'shop', {ready: '1/3', desired: 3, available: 1, up_to_date: 3, image: 'registry/api:1.8', tone: 'warn', hpa: true}),
      wl('Deployment', 'web', 'shop', {ready: '2/2', desired: 2, available: 2, up_to_date: 2, image: 'registry/web:4.2'}),
      wl('Deployment', 'worker', 'shop', {ready: '3/3', desired: 3, available: 3, up_to_date: 3, image: 'registry/worker:2.0', age_sec: 9 * 86400}),
      wl('StatefulSet', 'db', 'data', {age_sec: 17 * 86400, volumes: 'data-db-0 · 100 GiB'}),
      wl('StatefulSet', 'redis', 'shop', {age_sec: 17 * 86400, volumes: 'data-redis-0 · 5 GiB'}),
      wl('DaemonSet', 'node-exporter', 'monitoring', {ready: '2/3', desired: 3, available: 2, tone: 'warn', note: 'worker-2 is NotReady', age_sec: 96 * 86400}),
      wl('CronJob', 'cron-sync', 'data', {schedule: '*/15 * * * *', last_run_sec: 660, result: 'Failed', tone: 'bad', note: 'ImagePullBackOff'}),
      wl('CronJob', 'nightly-report', 'shop', {schedule: '0 2 * * *', last_run_sec: 9 * 3600, result: 'Completed', note: 'in 41 s'}),
      wl('Job', 'migrate-20261103', 'shop', {ready: '1/1', last_run_sec: 86400, result: 'Completed', note: '1 / 1'}),
    ],
    deployment: {
      workload: wl('Deployment', 'api', 'shop', {ready: '1/3', desired: 3, available: 1, up_to_date: 3, image: 'registry/api:1.8', tone: 'warn', hpa: true}),
      strategy: 'RollingUpdate', max_surge: '25%', max_unavailable: '25%', selector: 'app=api', paused: false, updated: 2, rolling: true, revision: 7,
      revisions: [
        {revision: 7, image: 'registry/api:1.8', cause: 'kubectl set image api=registry/api:1.8', age_sec: 3 * 86400, current: true},
        {revision: 6, image: 'registry/api:1.7.2', cause: 'kubectl set image api=registry/api:1.7.2', age_sec: 11 * 86400, current: false},
        {revision: 5, image: 'registry/api:1.7.1', cause: '', age_sec: 19 * 86400, current: false},
      ],
      hpa: {name: 'api', min: 2, max: 6, current: 3, cpu_pct: 84, target_cpu_pct: 70, last_scaled_sec: 360},
    },
    network: {
      services: [
        {name: 'api', namespace: 'shop', type: 'ClusterIP', cluster_ip: '10.96.14.8', ports: '80 → 3000/TCP', ready_endpoints: 1, not_ready_endpoints: 2},
        {name: 'web', namespace: 'shop', type: 'ClusterIP', cluster_ip: '10.96.21.40', ports: '80 → 8080/TCP', ready_endpoints: 2, not_ready_endpoints: 0},
        {name: 'redis', namespace: 'shop', type: 'ClusterIP', cluster_ip: 'None', ports: '6379/TCP', ready_endpoints: 1, not_ready_endpoints: 0},
        {name: 'legacy-lb', namespace: 'shop', type: 'LoadBalancer', cluster_ip: '10.96.7.2', ports: '443 → 8443/TCP', ready_endpoints: 0, not_ready_endpoints: 0},
      ],
      ingresses: [
        {name: 'shop', namespace: 'shop', host: 'shop.example.com', path: '/', service: 'web:80', class: 'nginx', tls_secret: 'shop-tls', cert_days_left: 9, address: '162.43.38.137'},
        {name: 'shop', namespace: 'shop', host: 'shop.example.com', path: '/api', service: 'api:80', class: 'nginx', tls_secret: 'shop-tls', cert_days_left: 9, address: '162.43.38.137'},
        {name: 'admin', namespace: 'shop', host: 'admin.example.com', path: '/', service: 'web:80', class: 'nginx', tls_secret: 'admin-tls', cert_days_left: 71, address: '162.43.38.137'},
      ],
    },
    config: {
      configmaps: [
        {name: 'api-config', namespace: 'shop', type: '', keys: ['LOG_LEVEL', 'DB_HOST', 'FEATURE_FLAGS'], used_by: ['api'], age_sec: 19 * 86400},
        {name: 'web-nginx', namespace: 'shop', type: '', keys: ['default.conf'], used_by: ['web'], age_sec: 19 * 86400},
      ],
      secrets: [
        {name: 'api-tls', namespace: 'shop', type: 'kubernetes.io/tls', keys: ['tls.crt', 'tls.key'], used_by: ['api'], age_sec: 71 * 86400},
        {name: 'db-credentials', namespace: 'shop', type: 'Opaque', keys: ['username', 'password'], used_by: ['api', 'worker'], age_sec: 96 * 86400},
      ],
      volumes: [
        {name: 'data-db-0', namespace: 'shop', status: 'Bound', size: '100 GiB', used_pct: 91, used: '91 of 100 GiB', storage_class: 'local-path', used_by: ['db-0']},
        {name: 'data-api', namespace: 'shop', status: 'Bound', size: '10 GiB', used_pct: 34, used: '3.4 of 10 GiB', storage_class: 'local-path', used_by: ['api']},
        {name: 'scratch-old', namespace: 'shop', status: 'Released', size: '5 GiB', used_pct: null, used: '', storage_class: 'local-path', used_by: []},
      ],
    },
    configMaps: {'api-config': {LOG_LEVEL: 'info', DB_HOST: 'db.data.svc', FEATURE_FLAGS: 'a,b'}, 'web-nginx': {'default.conf': 'server { listen 8080; }'}},
    secretValues: {'db-credentials/password': 'hunter2', 'db-credentials/username': 'shop'},
    events: [
      {age_sec: 40, type: 'Warning', reason: 'BackOff', object: 'pod/api-7d9f6c-x2kq8', namespace: 'shop', message: 'Back-off restarting failed container api in pod api-7d9f6c-x2kq8', count: 212},
      {age_sec: 120, type: 'Warning', reason: 'FailedScheduling', object: 'pod/worker-5c8b4-p9qzt', namespace: 'shop', message: '0/3 nodes are available: 1 node(s) had untolerated taint, 1 Insufficient memory, 1 node(s) were unschedulable.', count: 6},
      {age_sec: 660, type: 'Warning', reason: 'Failed', object: 'pod/cron-sync-28811-6lvdm', namespace: 'data', message: 'Failed to pull image "registry/sync:3.1": rpc error: code = NotFound', count: 3},
      {age_sec: 1080, type: 'Warning', reason: 'NodeNotReady', object: 'node/worker-2', namespace: '', message: 'Node worker-2 status is now: NodeNotReady', count: 1},
      {age_sec: 2820, type: 'Warning', reason: 'Unhealthy', object: 'pod/web-6b7d9-m4zwn', namespace: 'shop', message: 'Readiness probe failed: HTTP probe failed with statuscode: 503', count: 4},
      {age_sec: 300, type: 'Normal', reason: 'Pulled', object: 'pod/redis-0', namespace: 'shop', message: 'Container image "redis:7" already present on machine', count: 1},
      {age_sec: 7200, type: 'Warning', reason: 'OldNews', object: 'pod/gone', namespace: 'shop', message: 'older than an hour', count: 1},
    ],
    logs: '2026-11-04T11:41:07Z Error: ECONNREFUSED 10.96.0.15:5432 (db.data.svc)\n2026-11-04T11:41:08Z Fatal: could not open the database, retrying in 5s: ECONNREFUSED\n2026-11-04T11:41:09Z listening on :3000',
    yaml: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: shop\nspec:\n  replicas: 3\n',
    resources: [
      {group: '', version: 'v1', plural: 'nodes', kind: 'Node', namespaced: false},
      {group: '', version: 'v1', plural: 'pods', kind: 'Pod', namespaced: true},
      {group: 'cert-manager.io', version: 'v1', plural: 'certificates', kind: 'Certificate', namespaced: true, short: ['cert']},
    ],
    objects: {
      certificates: {namespaced: true, columns: ['Ready', 'Secret'], more: false, rows: [
        {namespace: 'shop', name: 'web', age_sec: 5 * 86400, cells: ['True', 'web-tls']},
        {namespace: 'data', name: 'db', age_sec: 3600, cells: ['False', 'db-tls']},
      ]},
      nodes: {namespaced: false, columns: [], more: false, rows: [{namespace: '', name: 'worker-1', age_sec: 40 * 86400, cells: []}]},
    },
    objectYaml: 'apiVersion: cert-manager.io/v1\nkind: Certificate\nmetadata:\n  name: web\n  namespace: shop\nspec:\n  secretName: web-tls\n',
    podFiles: {
      '/app': [
        {name: 'storage', path: '/app/storage', size: 4096, mode: 'drwxr-xr-x', mod_time: '2026-11-05T03:00:00Z', is_dir: true, is_link: false},
        {name: 'artisan', path: '/app/artisan', size: 1686, mode: '-rwxr-xr-x', mod_time: '2026-11-04T06:00:00Z', is_dir: false, is_link: false},
        {name: '.env', path: '/app/.env', size: 412, mode: '-rw-r--r--', mod_time: '2026-11-04T06:00:00Z', is_dir: false, is_link: false},
      ],
      '/app/storage': [
        {name: 'laravel.log', path: '/app/storage/laravel.log', size: 4072955, mode: '-rw-r--r--', mod_time: '2026-11-05T04:00:00Z', is_dir: false, is_link: false},
      ],
    },
    security: {pods: 3, services: 2, findings: [
      {severity: 'high', rule: 'privileged', kind: 'Deployment', namespace: 'shop', name: 'web', container: 'app', detail: 'runs privileged: it has the rights of root on the node', count: 2},
      {severity: 'medium', rule: 'plain-secret', kind: 'StatefulSet', namespace: 'data', name: 'db', container: 'pg', detail: 'has a value written in plain in the environment: DB_PASSWORD (a Secret keeps it out of the manifest)', count: 1},
      {severity: 'low', rule: 'image-tag', kind: 'Deployment', namespace: 'shop', name: 'web', container: 'app', detail: 'uses nginx: the tag is missing or latest, so the image can change under it', count: 2},
    ]},
    helm: [
      {name: 'web', namespace: 'shop', revision: 3, status: 'deployed', age_sec: 2 * 86400},
      {name: 'db', namespace: 'data', revision: 1, status: 'failed', age_sec: 3600},
    ],
    helmDetail: {
      web: {release: {name: 'web', namespace: 'shop', revision: 3, status: 'deployed', age_sec: 2 * 86400}, chart: 'nginx', chart_version: '15.1.0', app_version: '1.25.3', description: 'Upgrade complete', notes: 'Visit http://web', has_values: true,
        manifest: 'kind: Deployment\nmetadata:\n  name: web\n', history: [
          {name: 'web', namespace: 'shop', revision: 3, status: 'deployed', age_sec: 2 * 86400},
          {name: 'web', namespace: 'shop', revision: 2, status: 'superseded', age_sec: 9 * 86400},
        ]},
      db: {release: {name: 'db', namespace: 'data', revision: 1, status: 'failed', age_sec: 3600}, chart: 'postgresql', chart_version: '13.0.0', app_version: '16.1', description: 'Install failed', notes: '', has_values: false, manifest: '', history: [{name: 'db', namespace: 'data', revision: 1, status: 'failed', age_sec: 3600}]},
    },
    helmValues: 'replicas: 3\n',
    drain: {node: 'worker-1', moved: 14, daemonsets: 2, emptydir: ['cache-0', 'cache-1'], bare: ['batch-debug'], pdb: [{namespace: 'shop', name: 'api', detail: 'allows 1 pod down and this would take 2'}], blocked: true},
    forwards: [],
    spec: {kind: 'Deployment', name: 'api', namespace: 'shop', replicas: 3, service_account: 'api', containers: [
      {name: 'init-db', image: 'busybox:1.36', init: true, command: ['sh', '-c'], args: ['until nc -z db 5432; do sleep 1; done'], ports: [], env: [], env_from: [], requests: {}, limits: {}, mounts: []},
      {name: 'api', image: 'registry/api:1.8', init: false, command: [], args: ['--port', '3000'], ports: ['http 3000/TCP'],
        env: [{name: 'LOG_LEVEL', value: 'info', from: ''}, {name: 'DB_PASSWORD', value: '', from: 'secret db-credentials:password'}, {name: 'POD_NAME', value: '', from: 'field metadata.name'}],
        env_from: ['configmap api-config'], requests: {cpu: '250m', memory: '256Mi'}, limits: {cpu: '1', memory: '512Mi'}, mounts: ['config → /etc/api (read only)']},
      {name: 'nginx-sidecar', image: 'nginx:1.27', init: false, command: [], args: [], ports: ['80/TCP'], env: [], env_from: [], requests: {}, limits: {}, mounts: []},
    ]},
    editable: {yaml: 'apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: shop\n  resourceVersion: "812"\nspec:\n  replicas: 3\n', version: '812'},
    savedPath: '/Users/me/Downloads/shop-api-deployment.yaml',
    preview: {diff: [{op: ' ', text: 'spec:'}, {op: '-', text: '  replicas: 3'}, {op: '+', text: '  replicas: 4'}], changed: 2},
    previewError: '',
    shell: "kubectl exec -it -n shop web-6b7d9-m4zwn -c web --context prod-cluster -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'",
    debug: {container: 'vutty-debug-a1b2c3', command: "kubectl debug -it -n shop web-6b7d9-m4zwn --image=nicolaka/netshoot --context prod-cluster -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'"},
    fail: '',
    hold: null,
  }
}

type Rec = (name: string, ...a: unknown[]) => void
type K8sApi = Pick<Backend['server'], Extract<keyof Backend['server'], `k8s${string}` | `podFiles${string}`>>

/** The k8s part of the fake `be.server`: every call is recorded as `server.<name>` with its arguments after the session id. */
export function k8sApi(k: () => K8sFake, rec: Rec): K8sApi {
  const copy = <T,>(x: T): T => JSON.parse(JSON.stringify(x))
  const change = async (name: string, ...a: unknown[]) => {
    rec('server.' + name, ...a)
    if (k().hold) await k().hold
    if (k().fail) throw k().fail
  }
  const inNs = <T extends {namespace: string}>(l: T[], ns: string) => l.filter(x => !ns || x.namespace === ns)
  const podOf = (ns: string, name: string) => k().pods.find(p => p.name === name && (!ns || p.namespace === ns))
  let seq = 0
  return {
    k8sDetect: async s => { rec('server.k8sDetect', s); return copy(k().detect) },
    k8sContexts: async s => { rec('server.k8sContexts', s); return copy(k().contexts) },
    k8sCheck: async (s, c) => { rec('server.k8sCheck', c); return copy(k().checks[c] ?? k().checks.default) },
    k8sAccess: async (s, c, n) => { rec('server.k8sAccess', c, n); const r: Record<string, boolean> = {}; for (const id of ['list', 'logs', 'exec', 'portforward', 'scale', 'restart', 'delete_pod', 'cronjob_run', 'cronjob_patch', 'cordon', 'drain', 'secrets', 'edit', 'debug']) r[id] = k().rights[id] ?? true; return {rights: r} satisfies K8sAccess },
    k8sNamespaces: async (s, c) => { rec('server.k8sNamespaces', c); return {names: [...k().namespaces]} },
    k8sOverview: async (s, c, n) => { rec('server.k8sOverview', c, n); return copy(k().overview) },
    k8sNodes: async (s, c) => { rec('server.k8sNodes', c); return copy(k().nodes) },
    k8sPods: async (s, c, n) => { rec('server.k8sPods', c, n); return copy(inNs(k().pods, n)) },
    k8sPod: async (s, c, n, name): Promise<K8sPodDetail> => {
      rec('server.k8sPod', c, n, name)
      const p = podOf(n, name)
      if (!p) throw `pods "${name}" not found`
      return copy({pod: p, ip: '10.244.2.17', qos: 'Burstable', service_account: 'api', labels: ['app=api', 'tier=backend'], conditions: [{type: 'PodScheduled', status: 'True'}, {type: 'Ready', status: p.tone === 'ok' ? 'True' : 'False'}], volumes: [{name: 'config', source: 'ConfigMap api-config'}, {name: 'tls', source: 'Secret api-tls (values hidden)'}],
        events: k().events.filter(e => e.object === 'pod/' + name)})
    },
    k8sWorkloads: async (s, c, n) => { rec('server.k8sWorkloads', c, n); return {items: copy(inNs(k().workloads, n))} },
    k8sDeployment: async (s, c, n, name) => { rec('server.k8sDeployment', c, n, name); return copy(k().deployment) },
    k8sNetwork: async (s, c, n) => { rec('server.k8sNetwork', c, n); return copy({services: inNs(k().network.services, n), ingresses: inNs(k().network.ingresses, n)}) },
    k8sConfig: async (s, c, n) => { rec('server.k8sConfig', c, n); const x = k().config; return copy({configmaps: inNs(x.configmaps, n), secrets: inNs(x.secrets, n), volumes: inNs(x.volumes, n)}) },
    k8sConfigMap: async (s, c, n, name) => { rec('server.k8sConfigMap', c, n, name); return {...(k().configMaps[name] ?? {})} },
    k8sEvents: async (s, c, n) => { rec('server.k8sEvents', c, n); return copy(inNs(k().events, n)) },
    k8sLogs: async (s, c, n, p, o) => { rec('server.k8sLogs', c, n, p, o); return k().logs },
    k8sLogsMulti: async (s, c, n, kind, name, o): Promise<K8sMultiLogs> => { rec('server.k8sLogsMulti', c, n, kind, name, o); return {text: k().logs.split('\n').map((l, i) => `[${name}-${i % 2 + 1}] ${l}`).join('\n'), pods: 2, skipped: 0, failed: []} },
    k8sYAML: async (s, c, kind, n, name) => { rec('server.k8sYAML', c, kind, n, name); return k().yaml },
    k8sCounts: async (s, c, n) => { rec('server.k8sCounts', c, n); const x = k(); return {pods: inNs(x.pods, n).length, workloads: inNs(x.workloads, n).length, network: inNs(x.network.services, n).length + inNs(x.network.ingresses, n).length} },
    k8sDebugStart: async (s, c, n, p, i, t) => { rec('server.k8sDebugStart', c, n, p, i, t); if (k().fail) throw new Error(k().fail); return {...k().debug} },
    k8sSecurity: async (s, c, n) => { rec('server.k8sSecurity', c, n); const x = k().security; return copy({...x, findings: inNs(x.findings, n)}) },
    k8sHelmReleases: async (s, c, n) => { rec('server.k8sHelmReleases', c, n); return copy(inNs(k().helm, n)) },
    k8sHelmRelease: async (s, c, n, name, rev) => { rec('server.k8sHelmRelease', c, n, name, rev); const d = k().helmDetail[name]; if (!d) throw new Error('no such release'); return copy(rev ? {...d, release: {...d.release, revision: rev}} : d) },
    k8sHelmValues: async (s, c, n, name, rev) => { rec('server.k8sHelmValues', c, n, name, rev); return k().helmValues },
    podFilesHome: async h => { rec('server.podFilesHome', h); return '/app' },
    podFilesList: async (h, d) => { rec('server.podFilesList', h, d); return copy(k().podFiles[d] ?? []) },
    podFilesFetch: async (h, p, dir) => { rec('server.podFilesFetch', h, p, dir); return dir + '/' + p.slice(p.lastIndexOf('/') + 1) },
    k8sResources: async (s, c) => { rec('server.k8sResources', c); return copy(k().resources) },
    k8sObjects: async (s, c, n, ref) => { rec('server.k8sObjects', c, n, ref); const l = copy(k().objects[ref.plural] ?? {namespaced: true, columns: [], more: false, rows: []}); return {...l, rows: l.rows.filter(r => !n || !r.namespace || n.split(',').includes(r.namespace))} },
    k8sObjectYAML: async (s, c, n, ref, name) => { rec('server.k8sObjectYAML', c, n, ref, name); return k().objectYaml },
    k8sDrainPlan: async (s, c, node, o) => { rec('server.k8sDrainPlan', c, node, o); return copy({...k().drain, node}) },
    k8sScale: async (s, c, n, name, r) => {
      await change('k8sScale', c, n, name, r)
      k().workloads = k().workloads.map(w => (w.name === name && w.kind === 'Deployment' ? {...w, desired: r, ready: `${w.available}/${r}`} : w))
    },
    k8sRestart: async (s, c, n, kind, name) => change('k8sRestart', c, n, kind, name),
    k8sRolloutPause: async (s, c, n, name, pause) => { await change('k8sRolloutPause', c, n, name, pause); k().deployment = {...k().deployment, paused: pause} },
    k8sRollback: async (s, c, n, name, rev) => change('k8sRollback', c, n, name, rev),
    k8sDeletePod: async (s, c, n, name) => { await change('k8sDeletePod', c, n, name); k().pods = k().pods.filter(p => !(p.name === name && p.namespace === n)) },
    k8sDeleteJob: async (s, c, n, name) => { await change('k8sDeleteJob', c, n, name); k().workloads = k().workloads.filter(w => !(w.kind === 'Job' && w.name === name)) },
    k8sCronJob: async (s, c, n, name, action) => {
      await change('k8sCronJob', c, n, name, action)
      if (action !== 'run') k().workloads = k().workloads.map(w => (w.kind === 'CronJob' && w.name === name ? {...w, suspended: action === 'suspend'} : w))
    },
    k8sCordon: async (s, c, node, on) => { await change('k8sCordon', c, node, on); k().nodes = k().nodes.map(x => (x.name === node ? {...x, unschedulable: on} : x)) },
    k8sDrain: async (s, c, node, o) => { await change('k8sDrain', c, node, o); k().nodes = k().nodes.map(x => (x.name === node ? {...x, unschedulable: true} : x)) },
    k8sShellCommand: async (s, c, n, p, ctn) => { rec('server.k8sShellCommand', c, n, p, ctn); return k().shell },
    k8sShellSession: async (s, c, n, p, ctn) => { rec('server.k8sShellSession', c, n, p, ctn); return {host_id: 'kubeexec:t1', title: 'cluster · ' + p} },
    k8sWorkloadSpec: async (s, c, kind, n, name) => { rec('server.k8sWorkloadSpec', c, kind, n, name); return copy({...k().spec, name, namespace: n}) },
    k8sEditable: async (s, c, kind, n, name) => { rec('server.k8sEditable', c, kind, n, name); return copy(k().editable) },
    k8sSaveYAML: async (s, c, kind, n, name) => { rec('server.k8sSaveYAML', c, kind, n, name); return k().savedPath },
    k8sEditPreview: async (s, c, kind, n, name, y) => { rec('server.k8sEditPreview', c, kind, n, name, y); if (k().previewError) throw k().previewError; return copy(k().preview) },
    k8sEditApply: async (s, c, kind, n, name, y) => { await change('k8sEditApply', c, kind, n, name, y) },
    k8sSetContainer: async (s, c, kind, n, name, e: K8sContainerEdit) => { await change('k8sSetContainer', c, kind, n, name, e) },
    k8sAddSidecar: async (s, c, kind, n, name, sc: K8sSidecar) => { await change('k8sAddSidecar', c, kind, n, name, sc) },
    k8sPortForwardStart: async (s, c, n, service, local, remote) => {
      await change('k8sPortForwardStart', c, n, service, local, remote)
      const f: K8sForward = {id: 'pf' + ++seq, namespace: n, service, local_port: local, remote_port: remote, status: 'running', error: ''}
      k().forwards = [...k().forwards, f]
      return {...f}
    },
    k8sPortForwardStop: async (s, id) => { rec('server.k8sPortForwardStop', id); k().forwards = k().forwards.filter(f => f.id !== id) },
    k8sPortForwards: async () => copy(k().forwards),
    k8sSecretKey: async (s, c, n, name, key) => { await change('k8sSecretKey', c, n, name, key); return k().secretValues[name + '/' + key] ?? '' },
  }
}
