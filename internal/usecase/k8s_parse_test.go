package usecase

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

var k8sTestNow = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)

const podsJSON = `{"items":[
 {"metadata":{"name":"api-7d9f6c-x2kq8","namespace":"shop","creationTimestamp":"2026-11-01T12:00:00Z",
   "labels":{"app":"api"},"ownerReferences":[{"kind":"ReplicaSet","name":"api-7d9f6c"}]},
  "spec":{"nodeName":"worker-2","serviceAccountName":"api","containers":[{"name":"api","image":"registry/api:1.8",
    "resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"cpu":"1","memory":"512Mi"}},
    "livenessProbe":{"httpGet":{}},"readinessProbe":{"httpGet":{}}}],
   "volumes":[{"name":"config","configMap":{"name":"api-config"}},{"name":"tls","secret":{"secretName":"api-tls"}},{"name":"data","persistentVolumeClaim":{"claimName":"data-api"}}]},
  "status":{"phase":"Running","podIP":"10.244.2.17","qosClass":"Burstable","conditions":[{"type":"Ready","status":"False"}],
   "containerStatuses":[{"name":"api","ready":false,"restartCount":14,
     "state":{"waiting":{"reason":"CrashLoopBackOff","message":"back-off 5m0s restarting failed container"}},
     "lastState":{"terminated":{"exitCode":1,"reason":"Error"}}}]}},
 {"metadata":{"name":"web-1","namespace":"shop","creationTimestamp":"2026-11-04T11:00:00Z","ownerReferences":[{"kind":"ReplicaSet","name":"web-5c7d8b9f4"}]},
  "spec":{"nodeName":"worker-1","containers":[{"name":"web","image":"nginx:1.27"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"web","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2026-11-04T11:00:05Z"}}}]}},
 {"metadata":{"name":"batch-0","namespace":"shop","creationTimestamp":"2026-11-04T11:50:00Z"},
  "spec":{"containers":[{"name":"b","image":"batch:2"}]},
  "status":{"phase":"Pending","conditions":[{"type":"PodScheduled","status":"False","reason":"Unschedulable"}]}},
 {"metadata":{"name":"img-1","namespace":"data","creationTimestamp":"2026-11-04T11:00:00Z"},
  "spec":{"nodeName":"worker-1","containers":[{"name":"c","image":"registry/missing:9"}]},
  "status":{"phase":"Pending","containerStatuses":[{"name":"c","ready":false,"restartCount":0,
    "state":{"waiting":{"reason":"ImagePullBackOff","message":"Back-off pulling image \"registry/missing:9\""}}}]}},
 {"metadata":{"name":"migrate-abc","namespace":"shop","creationTimestamp":"2026-11-03T11:00:00Z"},
  "spec":{"nodeName":"worker-1","containers":[{"name":"m","image":"migrate:1"}]},
  "status":{"phase":"Succeeded","containerStatuses":[{"name":"m","ready":false,"restartCount":0,"state":{"terminated":{"exitCode":0,"reason":"Completed"}}}]}},
 {"metadata":{"name":"init-1","namespace":"shop","creationTimestamp":"2026-11-04T11:59:00Z"},
  "spec":{"nodeName":"worker-1","initContainers":[{"name":"wait","image":"busybox"}],"containers":[{"name":"c","image":"x"}]},
  "status":{"phase":"Pending","initContainerStatuses":[{"name":"wait","ready":false,"restartCount":0,"state":{"running":{}}}]}},
 {"metadata":{"name":"old-1","namespace":"shop","creationTimestamp":"2026-11-04T11:59:00Z","deletionTimestamp":"2026-11-04T11:59:30Z"},
  "spec":{"nodeName":"worker-1","containers":[{"name":"c","image":"x"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"c","ready":true,"restartCount":0,"state":{"running":{}}}]}},
 {"metadata":{"name":"oom-1","namespace":"shop","creationTimestamp":"2026-11-04T11:59:00Z"},
  "spec":{"nodeName":"worker-1","containers":[{"name":"c","image":"x"}]},
  "status":{"phase":"Running","containerStatuses":[{"name":"c","ready":false,"restartCount":3,
    "state":{"waiting":{"reason":"CrashLoopBackOff"}},"lastState":{"terminated":{"exitCode":137,"reason":"OOMKilled"}}}]}}
]}`

const eventsJSON = `{"items":[
 {"metadata":{"name":"e1","namespace":"shop","creationTimestamp":"2026-11-04T11:00:00Z"},"involvedObject":{"kind":"Pod","name":"batch-0","namespace":"shop"},
  "type":"Warning","reason":"FailedScheduling","message":"0/3 nodes are available: 3 Insufficient memory.","count":4,"lastTimestamp":"2026-11-04T11:55:00Z"},
 {"metadata":{"name":"e2","namespace":"shop","creationTimestamp":"2026-11-01T12:00:00Z"},"involvedObject":{"kind":"Pod","name":"api-7d9f6c-x2kq8","namespace":"shop"},
  "type":"Normal","reason":"Pulled","message":"Successfully pulled image","lastTimestamp":"2026-11-01T12:00:04Z"},
 {"metadata":{"name":"e3","namespace":"shop","creationTimestamp":"2026-11-04T11:59:20Z"},"involvedObject":{"kind":"Pod","name":"api-7d9f6c-x2kq8","namespace":"shop"},
  "type":"Warning","reason":"BackOff","message":"Back-off restarting failed container api","series":{"count":9,"lastObservedTime":"2026-11-04T11:59:20.123456Z"}}
]}`

func podByName(t *testing.T, ps []K8sPod, name string) K8sPod {
	t.Helper()
	for _, p := range ps {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no pod %s in %+v", name, ps)
	return K8sPod{}
}

func TestParsePodsStatusToneAndWhy(t *testing.T) {
	ps, err := parsePodList(podsJSON, eventsJSON, k8sTestNow)
	if err != nil || len(ps) != 8 {
		t.Fatalf("%v %d", err, len(ps))
	}
	crash := podByName(t, ps, "api-7d9f6c-x2kq8")
	if crash.Status != "CrashLoopBackOff" || crash.Tone != "bad" || crash.Restarts != 14 || crash.Ready != "0/1" || crash.AgeSec != 3*86400 {
		t.Fatalf("%+v", crash)
	}
	if !strings.Contains(crash.Why, "last exit code 1") || crash.Owner != "ReplicaSet api-7d9f6c ← Deployment api" || crash.Node != "worker-2" {
		t.Fatalf("%+v", crash)
	}
	c := crash.Containers[0]
	if c.State != "Waiting: CrashLoopBackOff, last exit 1" || c.Resources != "250m → 1 · 256Mi → 512Mi" || c.Probes != "liveness, readiness" || c.Restarts != 14 {
		t.Fatalf("%+v", c)
	}
	if ok := podByName(t, ps, "web-1"); ok.Status != "Running" || ok.Tone != "ok" || ok.Why != "" || ok.Ready != "1/1" {
		t.Fatalf("%+v", ok)
	}
	pend := podByName(t, ps, "batch-0")
	if pend.Status != "Pending" || pend.Tone != "warn" || !strings.Contains(pend.Why, "Insufficient memory") {
		t.Fatalf("%+v", pend)
	}
	img := podByName(t, ps, "img-1")
	if img.Status != "ImagePullBackOff" || img.Tone != "bad" || !strings.Contains(img.Why, "registry/missing:9") {
		t.Fatalf("%+v", img)
	}
	if done := podByName(t, ps, "migrate-abc"); done.Status != "Completed" && done.Status != "Succeeded" || done.Tone != "" {
		t.Fatalf("%+v", done)
	}
	if in := podByName(t, ps, "init-1"); in.Status != "Init:0/1" || in.Tone != "warn" {
		t.Fatalf("%+v", in)
	}
	if old := podByName(t, ps, "old-1"); old.Status != "Terminating" || old.Why != "being deleted" {
		t.Fatalf("%+v", old)
	}
	if oom := podByName(t, ps, "oom-1"); oom.Tone != "bad" || !strings.Contains(oom.Why, "memory") {
		t.Fatalf("%+v", oom)
	}
}

func TestParsePodDetail(t *testing.T) {
	one := strings.TrimSuffix(strings.TrimPrefix(podsJSON, `{"items":[`), `]}`)
	first := one[:strings.Index(one, `,
 {"metadata":{"name":"web-1"`)]
	d, err := parsePodDetail(first, eventsJSON, k8sTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if d.IP != "10.244.2.17" || d.QoS != "Burstable" || d.ServiceAccount != "api" || d.Labels[0] != "app=api" {
		t.Fatalf("%+v", d)
	}
	if len(d.Volumes) != 3 || d.Volumes[1].Source != "Secret api-tls (values hidden)" || d.Volumes[2].Source != "PVC data-api" || d.Volumes[0].Source != "ConfigMap api-config" {
		t.Fatalf("%+v", d.Volumes)
	}
	if len(d.Events) != 3 || d.Events[0].Reason != "BackOff" || d.Events[0].Count != 9 || d.Events[0].AgeSec != 39 || d.Events[2].Reason != "Pulled" {
		t.Fatalf("events newest first: %+v", d.Events)
	}
	if !strings.Contains(d.Pod.Why, "last exit code 1") {
		t.Fatalf("%q", d.Pod.Why)
	}
}

func TestParseNodes(t *testing.T) {
	nodes := `{"items":[
 {"metadata":{"name":"master-1","creationTimestamp":"2026-10-01T12:00:00Z","labels":{"node-role.kubernetes.io/control-plane":""}},
  "status":{"conditions":[{"type":"Ready","status":"True"}],"addresses":[{"type":"InternalIP","address":"10.0.0.10"}],"nodeInfo":{"kubeletVersion":"v1.29.2"},"allocatable":{"pods":"110","cpu":"4","memory":"8Gi"},"capacity":{"cpu":"4","memory":"8Gi"}}},
 {"metadata":{"name":"worker-2","creationTimestamp":"2026-10-01T12:00:00Z"},"spec":{"unschedulable":true},
  "status":{"conditions":[{"type":"Ready","status":"False"}],"addresses":[{"type":"InternalIP","address":"10.0.0.22"}],"allocatable":{"pods":"110"}}},
 {"metadata":{"name":"worker-3"},"status":{}}]}`
	top := "master-1   250m   6%    1800Mi   23%\nworker-2   <unknown>   <unknown>   <unknown>   <unknown>\n"
	ns, err := parseNodes(nodes, podsJSON, top, k8sTestNow)
	if err != nil || len(ns) != 3 {
		t.Fatalf("%v %+v", err, ns)
	}
	m, w, u := ns[0], ns[1], ns[2]
	if m.Status != "Ready" || m.Roles != "control-plane" || m.IP != "10.0.0.10" || m.CPUPct == nil || *m.CPUPct != 6 || *m.MemPct != 23 || m.PodsMax != 110 || m.Version != "v1.29.2" {
		t.Fatalf("%+v", m)
	}
	if w.Status != "NotReady" || !w.Unschedulable || w.CPUPct != nil || w.Pods != 1 {
		t.Fatalf("%+v", w)
	}
	if u.Status != "Unknown" {
		t.Fatalf("%+v", u)
	}
	if ns2, _ := parseNodes(nodes, "", "", k8sTestNow); ns2[0].CPUPct != nil {
		t.Fatal("without metrics the fields are null")
	}
}

const workJSON = `{"items":[
 {"kind":"Deployment","metadata":{"name":"api","namespace":"shop","creationTimestamp":"2026-10-01T12:00:00Z"},
  "spec":{"replicas":3,"template":{"spec":{"containers":[{"name":"api","image":"registry/api:1.9"}]}}},
  "status":{"replicas":4,"readyReplicas":2,"availableReplicas":2,"updatedReplicas":1,"unavailableReplicas":2}},
 {"kind":"Deployment","metadata":{"name":"web","namespace":"shop"},"spec":{"replicas":2,"template":{"spec":{"containers":[{"name":"w","image":"nginx"},{"name":"s","image":"side"}]}}},
  "status":{"replicas":2,"readyReplicas":2,"availableReplicas":2,"updatedReplicas":2}},
 {"kind":"Deployment","metadata":{"name":"dead","namespace":"shop"},"spec":{"replicas":2},"status":{"replicas":2,"unavailableReplicas":2,
  "conditions":[{"type":"Progressing","status":"False","reason":"ProgressDeadlineExceeded"}]}},
 {"kind":"StatefulSet","metadata":{"name":"db","namespace":"data"},"spec":{"replicas":1,"template":{"spec":{"containers":[{"name":"db","image":"pg:16"}]}},
  "volumeClaimTemplates":[{"metadata":{"name":"data"},"spec":{"resources":{"requests":{"storage":"100Gi"}}}}]},"status":{"replicas":1,"readyReplicas":1,"updatedReplicas":1}},
 {"kind":"DaemonSet","metadata":{"name":"node-exporter","namespace":"monitoring"},"spec":{"template":{"spec":{"containers":[{"name":"e","image":"ne"}]}}},
  "status":{"desiredNumberScheduled":3,"numberReady":2,"numberAvailable":2,"updatedNumberScheduled":3}},
 {"kind":"CronJob","metadata":{"name":"cron-sync","namespace":"data"},"spec":{"schedule":"*/15 * * * *","suspend":false,"jobTemplate":{"spec":{"template":{"spec":{"containers":[{"name":"c","image":"sync"}]}}}}},
  "status":{"lastScheduleTime":"2026-11-04T11:49:00Z","active":[]}},
 {"kind":"CronJob","metadata":{"name":"nightly","namespace":"shop"},"spec":{"schedule":"0 2 * * *","suspend":true},"status":{"lastScheduleTime":"2026-11-04T02:00:00Z"}},
 {"kind":"Job","metadata":{"name":"cron-sync-2847","namespace":"data","creationTimestamp":"2026-11-04T11:45:00Z","ownerReferences":[{"kind":"CronJob","name":"cron-sync"}]},
  "spec":{"completions":1},"status":{"failed":3,"conditions":[{"type":"Failed","status":"True","reason":"BackoffLimitExceeded"}]}},
 {"kind":"Job","metadata":{"name":"migrate-1103","namespace":"shop","creationTimestamp":"2026-11-03T11:00:00Z"},
  "spec":{"completions":1},"status":{"succeeded":1,"startTime":"2026-11-03T11:00:00Z","completionTime":"2026-11-03T11:00:41Z","conditions":[{"type":"Complete","status":"True"}]}},
 {"kind":"Pod","metadata":{"name":"stray"}}
]}`

const hpaJSON = `{"items":[{"metadata":{"name":"api","namespace":"shop"},"spec":{"scaleTargetRef":{"kind":"Deployment","name":"api"},"minReplicas":2,"maxReplicas":8,
 "metrics":[{"type":"Resource","resource":{"name":"cpu","target":{"averageUtilization":70}}}]},
 "status":{"currentReplicas":3,"lastScaleTime":"2026-11-04T10:00:00Z","currentMetrics":[{"type":"Resource","resource":{"name":"cpu","current":{"averageUtilization":64}}}]}}]}`

func workByName(t *testing.T, w K8sWorkloads, name string) K8sWorkload {
	t.Helper()
	for _, it := range w.Items {
		if it.Name == name {
			return it
		}
	}
	t.Fatalf("no workload %s", name)
	return K8sWorkload{}
}

func TestParseWorkloads(t *testing.T) {
	w, err := parseWorkloads(workJSON, hpaJSON, k8sTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Items) != 9 { // every Job is listed, also the one a CronJob made; the stray pod is not
		t.Fatalf("%d: %+v", len(w.Items), w.Items)
	}
	api := workByName(t, w, "api")
	if api.Ready != "2/3" || api.Tone != "warn" || !strings.Contains(api.Note, "rolling out") || !api.HPA || api.Image != "registry/api:1.9" || api.UpToDate != 1 {
		t.Fatalf("%+v", api)
	}
	if web := workByName(t, w, "web"); web.Tone != "ok" || web.Image != "nginx +1" || web.HPA {
		t.Fatalf("%+v", web)
	}
	if dead := workByName(t, w, "dead"); dead.Tone != "bad" || !strings.Contains(dead.Note, "progress deadline") {
		t.Fatalf("%+v", dead)
	}
	if db := workByName(t, w, "db"); db.Kind != "StatefulSet" || db.Ready != "1/1" || db.Volumes != "data-db-0 · 100 GiB" || db.Tone != "ok" {
		t.Fatalf("%+v", db)
	}
	if ds := workByName(t, w, "node-exporter"); ds.Ready != "2/3" || ds.Tone != "warn" || ds.Note != "1 not ready" {
		t.Fatalf("%+v", ds)
	}
	cs := workByName(t, w, "cron-sync")
	if cs.Schedule != "*/15 * * * *" || cs.LastRunSec == nil || *cs.LastRunSec != 660 || cs.Result != "Failed" || cs.Note != "BackoffLimitExceeded" || cs.Tone != "bad" {
		t.Fatalf("%+v", cs)
	}
	if n := workByName(t, w, "nightly"); !n.Suspended || n.Tone != "warn" || n.Result != "" {
		t.Fatalf("%+v", n)
	}
	if j := workByName(t, w, "cron-sync-2847"); j.Kind != "Job" || j.Owner != "CronJob cron-sync" || j.Result != "Failed" {
		t.Fatalf("a Job that a CronJob made is listed, with who made it: %+v", j)
	}
	if j := workByName(t, w, "migrate-1103"); j.Owner != "" {
		t.Fatalf("a Job nobody owns has no owner: %+v", j)
	}
	if j := workByName(t, w, "migrate-1103"); j.Result != "Completed" || j.Note != "in 41 s" || j.Ready != "1/1" || j.Tone != "ok" || j.LastRunSec == nil {
		t.Fatalf("%+v", j)
	}
}

func TestParseDeploymentDetail(t *testing.T) {
	dep := `{"metadata":{"name":"api","namespace":"shop","annotations":{"deployment.kubernetes.io/revision":"7"}},
 "spec":{"replicas":3,"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":"25%","maxUnavailable":1}},"selector":{"matchLabels":{"app":"api","tier":"backend"}},
  "template":{"spec":{"containers":[{"name":"api","image":"registry/api:1.9"}]}}},
 "status":{"replicas":4,"readyReplicas":2,"availableReplicas":2,"updatedReplicas":1,"unavailableReplicas":2}}`
	rs := `{"items":[
 {"metadata":{"name":"api-aaa","creationTimestamp":"2026-11-03T12:00:00Z","annotations":{"deployment.kubernetes.io/revision":"6","kubernetes.io/change-cause":"bump to 1.8"},"ownerReferences":[{"kind":"Deployment","name":"api"}]},
  "spec":{"template":{"spec":{"containers":[{"image":"registry/api:1.8"}]}}}},
 {"metadata":{"name":"api-bbb","creationTimestamp":"2026-11-04T11:00:00Z","annotations":{"deployment.kubernetes.io/revision":"7"},"ownerReferences":[{"kind":"Deployment","name":"api"}]},
  "spec":{"template":{"spec":{"containers":[{"image":"registry/api:1.9"}]}}}},
 {"metadata":{"name":"other-ccc","annotations":{"deployment.kubernetes.io/revision":"9"},"ownerReferences":[{"kind":"Deployment","name":"other"}]}}]}`
	d, err := parseDeployment(dep, rs, hpaJSON, k8sTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if d.Strategy != "RollingUpdate" || d.MaxSurge != "25%" || d.MaxUnavailable != "1" || d.Selector != "app=api,tier=backend" || !d.Rolling || d.Revision != 7 {
		t.Fatalf("%+v", d)
	}
	if len(d.Revisions) != 2 || d.Revisions[0].Revision != 7 || !d.Revisions[0].Current || d.Revisions[1].Cause != "bump to 1.8" || d.Revisions[1].Image != "registry/api:1.8" {
		t.Fatalf("%+v", d.Revisions)
	}
	if d.HPA == nil || d.HPA.Min != 2 || d.HPA.Max != 8 || *d.HPA.CPUPct != 64 || *d.HPA.TargetCPUPct != 70 || d.HPA.LastScaledSec == nil || *d.HPA.LastScaledSec != 7200 || !d.Workload.HPA {
		t.Fatalf("%+v", d.HPA)
	}
	if _, err := parseDeployment("nonsense", "", "", k8sTestNow); err == nil {
		t.Fatal("not JSON must be an error")
	}
}

func selfSignedPEM(t *testing.T, notAfter time.Time) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "shop.example.com"}, NotBefore: notAfter.Add(-90 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestCertDaysLeft(t *testing.T) {
	if d, ok := certDaysLeft(selfSignedPEM(t, k8sTestNow.Add(9*24*time.Hour+3*time.Hour)), k8sTestNow); !ok || d != 9 {
		t.Fatalf("%d %v", d, ok)
	}
	if d, ok := certDaysLeft(selfSignedPEM(t, k8sTestNow.Add(-2*time.Hour)), k8sTestNow); !ok || d >= 0 {
		t.Fatalf("an expired certificate has negative days: %d %v", d, ok)
	}
	for _, bad := range []string{"", "<no value>", "bm90IGEgY2VydA==", "!!!"} {
		if _, ok := certDaysLeft(bad, k8sTestNow); ok {
			t.Fatalf("%q", bad)
		}
	}
}

func TestParseNetwork(t *testing.T) {
	svc := `{"items":[
 {"kind":"Service","metadata":{"name":"api","namespace":"shop"},"spec":{"type":"ClusterIP","clusterIP":"10.96.14.8","ports":[{"port":80,"targetPort":3000,"protocol":"TCP"},{"port":443,"targetPort":"https"}]}},
 {"kind":"Service","metadata":{"name":"redis","namespace":"shop"},"spec":{"type":"ClusterIP","clusterIP":"None","ports":[{"port":6379,"targetPort":6379}]}},
 {"kind":"Service","metadata":{"name":"legacy-lb","namespace":"shop"},"spec":{"type":"LoadBalancer","clusterIP":"10.96.7.2","ports":[{"port":443,"targetPort":8443}]}},
 {"kind":"Endpoints","metadata":{"name":"api","namespace":"shop"},"subsets":[{"addresses":[{"ip":"1"}],"notReadyAddresses":[{"ip":"2"},{"ip":"3"}]}]},
 {"kind":"Endpoints","metadata":{"name":"redis","namespace":"shop"},"subsets":[{"addresses":[{"ip":"4"}]}]},
 {"kind":"Endpoints","metadata":{"name":"legacy-lb","namespace":"shop"}}]}`
	ing := `{"items":[{"metadata":{"name":"shop","namespace":"shop","annotations":{"kubernetes.io/ingress.class":"nginx"}},
 "spec":{"tls":[{"hosts":["shop.example.com"],"secretName":"shop-tls"}],"rules":[
  {"host":"shop.example.com","http":{"paths":[{"path":"/","backend":{"service":{"name":"web","port":{"number":80}}}},{"path":"/api","backend":{"service":{"name":"api","port":{"number":80}}}}]}},
  {"host":"plain.example.com","http":{"paths":[{"backend":{"service":{"name":"web","port":{"name":"http"}}}}]}}]},
 "status":{"loadBalancer":{"ingress":[{"ip":"162.43.38.137"}]}}}]}`
	n, err := parseNetwork(svc, ing, map[string]int{"shop/shop-tls": 9})
	if err != nil || len(n.Services) != 3 || len(n.Ingresses) != 3 {
		t.Fatalf("%v %+v", err, n)
	}
	a := n.Services[0]
	if a.Ports != "80 → 3000/TCP, 443 → https/TCP" || a.ReadyEndpoints != 1 || a.NotReadyEndpoints != 2 || a.ClusterIP != "10.96.14.8" {
		t.Fatalf("%+v", a)
	}
	if r := n.Services[1]; r.Ports != "6379/TCP" || r.ClusterIP != "None" || r.ReadyEndpoints != 1 {
		t.Fatalf("%+v", r)
	}
	if l := n.Services[2]; l.ReadyEndpoints != 0 || l.Type != "LoadBalancer" {
		t.Fatalf("%+v", l)
	}
	i0, i2 := n.Ingresses[0], n.Ingresses[2]
	if i0.Host != "shop.example.com" || i0.Path != "/" || i0.Service != "web:80" || i0.Class != "nginx" || i0.TLSSecret != "shop-tls" || i0.CertDaysLeft == nil || *i0.CertDaysLeft != 9 || i0.Address != "162.43.38.137" {
		t.Fatalf("%+v", i0)
	}
	if i2.TLSSecret != "" || i2.CertDaysLeft != nil || i2.Path != "/" || i2.Service != "web:http" {
		t.Fatalf("%+v", i2)
	}
}

func TestParseConfigKeysNeverHoldValues(t *testing.T) {
	cm := "shop|api-config|2026-11-01T12:00:00Z|LOG_LEVEL,PORT,|bin,\n"
	sec := "shop|api-tls|2026-11-02T12:00:00Z|tls.crt,tls.key,||kubernetes.io/tls\nshop|bare|2026-11-02T12:00:00Z|||<no value>\n\ngarbage\n"
	pvc := `{"items":[{"metadata":{"name":"data-api","namespace":"shop"},"spec":{"storageClassName":"fast","resources":{"requests":{"storage":"10Gi"}}},"status":{"phase":"Bound","capacity":{"storage":"10Gi"}}},
 {"metadata":{"name":"lost","namespace":"shop"},"status":{"phase":"Pending"}}]}`
	c, err := parseConfig(cm, sec, pvc, podsJSON, k8sTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.ConfigMaps) != 1 || len(c.ConfigMaps[0].Keys) != 3 || c.ConfigMaps[0].Type != "" || c.ConfigMaps[0].UsedBy[0] != "api" {
		t.Fatalf("%+v", c.ConfigMaps)
	}
	if len(c.Secrets) != 2 || c.Secrets[0].Type != "kubernetes.io/tls" || strings.Join(c.Secrets[0].Keys, ",") != "tls.crt,tls.key" || c.Secrets[0].UsedBy[0] != "api" || c.Secrets[1].Type != "" {
		t.Fatalf("%+v", c.Secrets)
	}
	if len(c.Volumes) != 2 || c.Volumes[0].Size != "10 GiB" || c.Volumes[0].StorageClass != "fast" || c.Volumes[0].UsedPct != nil || c.Volumes[0].UsedBy[0] != "api" || c.Volumes[1].Status != "Pending" {
		t.Fatalf("%+v", c.Volumes)
	}
	m, err := parseConfigMap(`{"data":{"a":"1"},"binaryData":{"b":"AAAA"}}`)
	if err != nil || m["a"] != "1" || !strings.HasPrefix(m["b"], "<binary") {
		t.Fatalf("%v %v", m, err)
	}
}

func TestParseEventsSortsNewestFirst(t *testing.T) {
	ev, err := parseEvents(eventsJSON, k8sTestNow)
	if err != nil || len(ev) != 3 {
		t.Fatalf("%v %+v", err, ev)
	}
	if ev[0].Reason != "BackOff" || ev[1].Reason != "FailedScheduling" || ev[1].Count != 4 || ev[0].Object != "pod/api-7d9f6c-x2kq8" || ev[2].Type != "Normal" || ev[1].Namespace != "shop" {
		t.Fatalf("%+v", ev)
	}
}

func TestOverviewCountsAndAttention(t *testing.T) {
	nodes := `{"items":[
 {"metadata":{"name":"a"},"status":{"conditions":[{"type":"Ready","status":"True"}],"allocatable":{"cpu":"4","memory":"8Gi","pods":"110"},"capacity":{"cpu":"4","memory":"8Gi"}}},
 {"metadata":{"name":"b"},"spec":{"unschedulable":true},"status":{"conditions":[{"type":"Ready","status":"False"}],"allocatable":{"cpu":"4","memory":"8Gi"},"capacity":{"cpu":"4","memory":"8Gi"}}}]}`
	pods := `{"items":[
 {"metadata":{"name":"ok","namespace":"x","creationTimestamp":"2026-11-04T11:00:00Z"},"spec":{"containers":[{"name":"c","image":"i","resources":{"requests":{"cpu":"1","memory":"2Gi"}}}]},"status":{"phase":"Running","containerStatuses":[{"name":"c","ready":true,"state":{"running":{}}}]}},
 {"metadata":{"name":"bad","namespace":"x","creationTimestamp":"2026-11-04T11:00:00Z"},"spec":{"containers":[{"name":"c","image":"i"}]},"status":{"phase":"Running","containerStatuses":[{"name":"c","restartCount":5,"state":{"waiting":{"reason":"CrashLoopBackOff"}}}]}},
 {"metadata":{"name":"stuck","namespace":"y","creationTimestamp":"2026-11-04T11:00:00Z"},"spec":{"containers":[{"name":"c","image":"i"}]},"status":{"phase":"Pending"}}]}`
	deps := `{"items":[{"metadata":{"name":"d","namespace":"x"},"spec":{"replicas":2},"status":{}}]}`
	pvcs := `{"items":[{"metadata":{"name":"v","namespace":"x"},"status":{"phase":"Pending"}}]}`
	soon, gone := 5, -1
	ings := []K8sIngress{{Name: "i", Namespace: "x", Host: "h", CertDaysLeft: &soon}, {Name: "j", Namespace: "x", Host: "k", CertDaysLeft: &gone}, {Name: "n", CertDaysLeft: nil}}
	o, err := k8sOverviewFrom(nodes, pods, deps, pvcs, ings, "v1.29.2", k8sTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if o.Nodes.Total != 2 || o.Nodes.Ready != 1 || o.Nodes.Cordoned != 1 || o.Pods.Total != 3 || o.Pods.Running != 1 || o.Pods.Failing != 1 || o.Pods.Pending != 1 {
		t.Fatalf("%+v", o)
	}
	if o.CPURequestedPct == nil || *o.CPURequestedPct != 13 || *o.MemRequestedPct != 13 || *o.CPUCores != 8 || *o.MemGiB != 16 || o.ServerVersion != "v1.29.2" {
		t.Fatalf("%v %v %v %v", o.CPURequestedPct, o.MemRequestedPct, o.CPUCores, o.MemGiB)
	}
	kinds := map[string]int{}
	for _, a := range o.Attention {
		kinds[a.Kind+"/"+a.Severity]++
	}
	for _, want := range []string{"node/bad", "node/warn", "pod/bad", "pod/warn", "deployment/bad", "pvc/warn", "ingress/warn", "ingress/bad"} {
		if kinds[want] == 0 {
			t.Errorf("no %s in %v", want, kinds)
		}
	}
	if o.Attention[0].Severity != "bad" || o.Namespaces[0].Name != "x" || o.Namespaces[0].Restarts != 5 || o.Namespaces[0].NotReady != 1 {
		t.Fatalf("%+v %+v", o.Attention[0], o.Namespaces)
	}
}

func TestPlanDrain(t *testing.T) {
	pods := `{"items":[
 {"metadata":{"name":"api-1","namespace":"shop","labels":{"app":"api"},"ownerReferences":[{"kind":"ReplicaSet","name":"api-7d"}]},"spec":{"containers":[{"name":"c"}]},"status":{"phase":"Running"}},
 {"metadata":{"name":"api-2","namespace":"shop","labels":{"app":"api"},"ownerReferences":[{"kind":"ReplicaSet","name":"api-7d"}]},"spec":{"containers":[{"name":"c"}]},"status":{"phase":"Running"}},
 {"metadata":{"name":"ne-1","namespace":"mon","ownerReferences":[{"kind":"DaemonSet","name":"ne"}]},"spec":{},"status":{"phase":"Running"}},
 {"metadata":{"name":"cache-0","namespace":"shop","ownerReferences":[{"kind":"StatefulSet","name":"cache"}]},"spec":{"volumes":[{"name":"s","emptyDir":{}}]},"status":{"phase":"Running"}},
 {"metadata":{"name":"batch-debug","namespace":"shop"},"spec":{},"status":{"phase":"Running"}},
 {"metadata":{"name":"done","namespace":"shop","ownerReferences":[{"kind":"Job","name":"j"}]},"spec":{},"status":{"phase":"Succeeded"}},
 {"metadata":{"name":"kube-apiserver","namespace":"kube-system","annotations":{"kubernetes.io/config.mirror":"x"}},"spec":{},"status":{"phase":"Running"}}]}`
	pdb := `{"items":[{"metadata":{"name":"api","namespace":"shop"},"spec":{"minAvailable":2,"selector":{"matchLabels":{"app":"api"}}},"status":{"disruptionsAllowed":1}},
 {"metadata":{"name":"other","namespace":"shop"},"spec":{"selector":{"matchLabels":{"app":"zzz"}}},"status":{}}]}`
	p, err := planDrain("worker-1", pods, pdb, K8sDrainOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Moved != 2 || p.Daemonsets != 1 || len(p.EmptyDir) != 1 || p.EmptyDir[0] != "shop/cache-0" || len(p.Bare) != 1 || p.Bare[0] != "shop/batch-debug" || !p.Blocked {
		t.Fatalf("%+v", p)
	}
	if len(p.PDB) != 1 || p.PDB[0].Name != "api" || !strings.Contains(p.PDB[0].Detail, "allows 1") || !strings.Contains(p.PDB[0].Detail, "needs 2") {
		t.Fatalf("%+v", p.PDB)
	}
	if p2, _ := planDrain("w", pods, "", K8sDrainOpts{Force: true}); p2.Blocked {
		t.Fatal("force unblocks")
	}
}

func TestClassifyErrors(t *testing.T) {
	for msg, want := range map[string]string{
		"Unable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout":                                 "unreachable",
		"The connection to the server 10.0.0.1:6443 was refused - did you specify the right host or port?":     "other",
		"dial tcp 127.0.0.1:6443: connect: connection refused":                                                 "unreachable",
		"dial tcp 10.0.0.1:6443: connect: no route to host":                                                    "unreachable",
		"error: You must be logged in to the server (Unauthorized)":                                            "unauthorized",
		"Unable to connect to the server: getting credentials: exec: executable aws failed with exit code 255": "unauthorized",
		"Unable to connect to the server: x509: certificate signed by unknown authority":                       "certificate",
		"x509: certificate has expired or is not yet valid":                                                    "certificate",
		`pods is forbidden: User "u" cannot list resource "pods" in API group "" at the cluster scope`:         "forbidden",
		"something else": "other",
	} {
		if got := k8sClassify(msg); got != want {
			t.Errorf("%q: %s, want %s", msg, got, want)
		}
	}
	if !k8sKubeconfigProblem(`error loading config file "/etc/rancher/k3s/k3s.yaml": open /etc/rancher/k3s/k3s.yaml: permission denied`) {
		t.Error("permission denied")
	}
}

func TestQuantities(t *testing.T) {
	for in, want := range map[string]float64{"250m": 0.25, "2": 2, "1500m": 1.5, "256Mi": 256 << 20, "1Gi": 1 << 30, "1G": 1e9, "": 0, "abc": 0, "1e3": 0} {
		if got := parseQty(in); got != want {
			t.Errorf("%q: %v want %v", in, got, want)
		}
	}
	if prettySize("100Gi") != "100 GiB" || prettySize("weird") != "weird" {
		t.Error("prettySize")
	}
}

// Odd output of kubectl must never panic: a list of nothing, null items, the wrong types.
func TestParsersSurviveOddInput(t *testing.T) {
	inputs := []string{"", "{}", "[]", "null", "garbage", `{"items":null}`, `{"items":[{}]}`, `{"items":[null]}`, `{"items":[{"metadata":null,"spec":null,"status":null}]}`,
		`{"items":[{"metadata":{"name":5}}]}`, `{"items":[{"status":{"containerStatuses":[{}],"initContainerStatuses":[{}]}}]}`, `{"items":[{"kind":"CronJob","status":{"active":"x"}}]}`}
	for _, in := range inputs {
		_, _ = parsePodList(in, in, k8sTestNow)
		_, _ = parsePodDetail(in, in, k8sTestNow)
		_, _ = parseNodes(in, in, in, k8sTestNow)
		_, _ = parseWorkloads(in, in, k8sTestNow)
		_, _ = parseDeployment(in, in, in, k8sTestNow)
		_, _ = parseNetwork(in, in, nil)
		_, _ = parseConfig(in, in, in, in, k8sTestNow)
		_, _ = parseConfigMap(in)
		_, _ = parseEvents(in, k8sTestNow)
		_, _ = k8sOverviewFrom(in, in, in, in, nil, "", k8sTestNow)
		_, _ = planDrain("n", in, in, K8sDrainOpts{})
		_ = parseConfigKeys(in, k8sTestNow)
		_ = parseTop(in)
		_ = secretYAML(in)
		_ = stripManagedFields(in)
	}
}
