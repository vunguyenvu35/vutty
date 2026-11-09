package usecase

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"vutty/internal/domain"
	"vutty/internal/kubeapi"
)

// ---- a connector made of internal/kubeapi, as the app builds it ----

type testKube struct{}

func (testKube) Contexts(data []byte) ([]KubeContext, map[string]string, error) {
	cfg, err := kubeapi.Parse(data)
	if err != nil {
		return nil, nil, err
	}
	var out []KubeContext
	problems := map[string]string{}
	for _, c := range cfg.List() {
		if err := cfg.Check(c.Name); err != nil {
			problems[c.Name] = err.Error()
			continue
		}
		out = append(out, KubeContext{Name: c.Name, Cluster: c.Cluster, Server: c.Server, User: c.User, Current: c.Current, Auth: c.Auth, Exec: c.Exec})
	}
	return out, problems, nil
}

func (testKube) Open(data []byte, ctx string, approve func(string) bool) (KubeAPI, error) {
	cfg, err := kubeapi.Parse(data)
	if err != nil {
		return nil, err
	}
	c, err := kubeapi.New(cfg, ctx, kubeapi.Options{ApproveExec: func(e kubeapi.ExecSpec) bool { return approve(e.String()) }})
	if err != nil {
		return nil, err
	}
	return testKubeClient{c}, nil
}

type testKubeClient struct{ *kubeapi.Client }

func (k testKubeClient) Exec(ctx context.Context, ns, pod, container string, command []string, cols, rows int, onData func([]byte)) (Shell, error) {
	s, err := k.Client.Exec(ctx, ns, pod, container, command, cols, rows, onData)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ---- a cluster that answers from fixtures, and remembers what it was asked ----

type fakeCluster struct {
	*httptest.Server
	mu    sync.Mutex
	calls []string // METHOD path?query body
	allow map[string]bool
	eph   string // the name of the ephemeral container that was added last
}

func (f *fakeCluster) called(method, prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, method+" "+prefix) {
			out = append(out, c)
		}
	}
	return out
}

func itemsOf(t *testing.T, doc, kind string) string {
	t.Helper()
	var l struct{ Items []map[string]any }
	if err := json.Unmarshal([]byte(doc), &l); err != nil {
		t.Fatal(err)
	}
	out := []map[string]any{}
	for _, it := range l.Items {
		if it["kind"] == kind {
			delete(it, "kind") // the API server does not put the kind on the items of a list
			out = append(out, it)
		}
	}
	b, _ := json.Marshal(map[string]any{"items": out})
	return string(b)
}

func newFakeCluster(t *testing.T) *fakeCluster {
	t.Helper()
	f := &fakeCluster{allow: map[string]bool{"secrets": false}}
	secret := `{"kind":"Secret","type":"Opaque","metadata":{"name":"api-tls","namespace":"shop","managedFields":[{"manager":"x"}],
	  "annotations":{"kubectl.kubernetes.io/last-applied-configuration":"PLAINTEXT-COPY"}},"data":{"password":"` + base64.StdEncoding.EncodeToString([]byte("hunter2")) + `","user":"YWRtaW4="}}`
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"kind":"Status","message":"Unauthorized"}`))
			return
		}
		p := r.URL.Path
		ok := func(s string) { _, _ = w.Write([]byte(s)) }
		switch {
		case strings.HasSuffix(p, "/ephemeralcontainers"):
			var b struct {
				Spec struct {
					E []struct{ Name string } `json:"ephemeralContainers"`
				} `json:"spec"`
			}
			_ = json.Unmarshal(body, &b)
			f.mu.Lock()
			if len(b.Spec.E) > 0 {
				f.eph = b.Spec.E[0].Name
			}
			f.mu.Unlock()
			ok(`{}`)
		case strings.HasSuffix(p, "/pods/dbg") || strings.HasSuffix(p, "/pods/dbgbad"):
			f.mu.Lock()
			name := f.eph
			f.mu.Unlock()
			if strings.HasSuffix(p, "/pods/dbg") {
				ok(`{"status":{"ephemeralContainerStatuses":[{"name":"` + name + `","state":{"running":{}}}]}}`)
			} else {
				ok(`{"status":{"ephemeralContainerStatuses":[{"name":"` + name + `","state":{"waiting":{"reason":"ImagePullBackOff","message":"pull access denied"}}}]}}`)
			}
		case strings.HasSuffix(p, "/portforward"):
			f.serveForward(w, r)
		case strings.HasSuffix(p, "/services/web-svc"):
			ok(`{"spec":{"selector":{"app":"web","tier":"fe"},"ports":[{"port":80,"targetPort":"http"},{"port":81,"targetPort":8081},{"port":90}]}}`)
		case strings.HasSuffix(p, "/services/headless"):
			ok(`{"spec":{"ports":[{"port":80}]}}`)
		case strings.HasSuffix(p, "/services/lonely"):
			ok(`{"spec":{"selector":{"app":"lonely"},"ports":[{"port":80}]}}`)
		case strings.HasSuffix(p, "/pods") && strings.Contains(r.URL.RawQuery, "labelSelector=app%3Dweb%2Ctier%3Dfe"):
			ok(`{"items":[` +
				`{"metadata":{"name":"web-0"},"spec":{"containers":[{"ports":[{"name":"http","containerPort":8080}]}]},"status":{"conditions":[{"type":"Ready","status":"False"}]}},` +
				`{"metadata":{"name":"web-gone","deletionTimestamp":"2026-11-05T00:00:00Z"},"spec":{"containers":[{"ports":[{"name":"http","containerPort":8080}]}]},"status":{"conditions":[{"type":"Ready","status":"True"}]}},` +
				`{"metadata":{"name":"web-1"},"spec":{"containers":[{"ports":[{"name":"http","containerPort":8080}]}]},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`)
		case strings.HasSuffix(p, "/pods") && strings.Contains(r.URL.RawQuery, "labelSelector=app%3Dlonely"):
			ok(`{"items":[]}`)
		case strings.HasSuffix(p, "/exec"):
			if strings.HasSuffix(p, "/pods/multi/exec") && r.URL.Query().Get("container") == "" {
				w.WriteHeader(400)
				ok(`{"kind":"Status","message":"a container name must be specified for pod multi, choose one of: [aws-ssm nginx]"}`)
				return
			}
			if r.URL.Query().Get("tty") == "false" {
				f.serveRun(w, r)
				return
			}
			f.serveExec(w, r)
		case strings.HasSuffix(p, "/pods/multi/log") || strings.HasSuffix(p, "/pods/tagged/log"):
			if r.URL.Query().Get("container") == "" {
				w.WriteHeader(400)
				ok(`{"kind":"Status","message":"a container name must be specified for pod multi, choose one of: [aws-ssm nginx]"}`)
				return
			}
			ok("log of " + r.URL.Query().Get("container") + "\n")
		case strings.HasSuffix(p, "/pods/multi"):
			ok(`{"metadata":{"name":"multi"},"spec":{"containers":[{"name":"aws-ssm"},{"name":"nginx"}]}}`)
		case strings.HasSuffix(p, "/pods/tagged"):
			ok(`{"metadata":{"name":"tagged","annotations":{"kubectl.kubernetes.io/default-container":"nginx"}},"spec":{"containers":[{"name":"aws-ssm"},{"name":"nginx"}]}}`)
		case p == "/version":
			ok(`{"major":"1","minor":"30","gitVersion":"v1.30.2"}`)
		case p == "/api/v1", strings.HasPrefix(p, "/apis/cert-manager.io"), strings.HasPrefix(p, "/apis/apiextensions.k8s.io/v1/customresourcedefinitions/"), p == "/apis":
			if body, found := discoveryFixtures[r.URL.RequestURI()]; found {
				ok(body)
				return
			}
			w.WriteHeader(404)
			ok(`{"kind":"Status","message":"not found: ` + p + `"}`)
		case strings.HasPrefix(p, "/api/v1/") && strings.Contains(r.URL.RawQuery, "owner%3Dhelm"):
			ok(helmItems(r.URL.Query().Get("labelSelector")))
		case strings.Contains(p, "/secrets/sh.helm.release.v1.web.v"):
			if strings.HasSuffix(p, ".v9") {
				w.WriteHeader(404)
				ok(`{"kind":"Status","message":"secrets not found"}`)
				return
			}
			ok(`{"data":{"release":"` + helmEnc(t, helmRelease(3, "deployed")) + `"}}`)
		case p == "/api/v1/namespaces":
			ok(`{"items":[{"metadata":{"name":"shop"}},{"metadata":{"name":"kube-system"}}]}`)
		case strings.HasSuffix(p, "/events"):
			ok(eventsJSON)
		case p == "/api/v1/pods" && r.URL.Query().Get("fieldSelector") != "":
			ok(`{"items":[]}`)
		case strings.HasSuffix(p, "/pods"):
			ok(podsJSON)
		case strings.HasSuffix(p, "/pods/api-7d9f6c-x2kq8/log"):
			ok("line one\nline two\n")
		case strings.HasSuffix(p, "/pods/api-7d9f6c-x2kq8"):
			one := strings.TrimSuffix(strings.TrimPrefix(podsJSON, `{"items":[`), `]}`)
			var l []json.RawMessage
			_ = json.Unmarshal([]byte("["+one+"]"), &l)
			_, _ = w.Write(l[0])
		case p == "/api/v1/nodes":
			ok(`{"items":[{"metadata":{"name":"worker-2"},"status":{"allocatable":{"cpu":"4","memory":"8Gi"},"conditions":[{"type":"Ready","status":"True"}]}}]}`)
		case p == "/apis/metrics.k8s.io/v1beta1/nodes":
			w.WriteHeader(404)
			ok(`{"kind":"Status","message":"the server could not find the requested resource"}`)
		case strings.HasSuffix(p, "/deployments"):
			ok(itemsOf(t, workJSON, "Deployment"))
		case strings.HasSuffix(p, "/statefulsets"):
			ok(itemsOf(t, workJSON, "StatefulSet"))
		case strings.HasSuffix(p, "/daemonsets"):
			ok(itemsOf(t, workJSON, "DaemonSet"))
		case strings.HasSuffix(p, "/cronjobs"):
			ok(itemsOf(t, workJSON, "CronJob"))
		case strings.HasSuffix(p, "/jobs") && r.Method == "GET":
			ok(itemsOf(t, workJSON, "Job"))
		case strings.HasSuffix(p, "/horizontalpodautoscalers"):
			ok(hpaJSON)
		case strings.HasSuffix(p, "/configmaps"):
			ok(`{"items":[{"metadata":{"name":"api-config","namespace":"shop"},"data":{"LOG":"debug","DB_PASS":"s3cret-value"}}]}`)
		case strings.HasSuffix(p, "/secrets"):
			ok(`{"items":[` + secret + `]}`)
		case strings.HasSuffix(p, "/secrets/api-tls"):
			ok(secret)
		case strings.HasSuffix(p, "/persistentvolumeclaims"), strings.HasSuffix(p, "/ingresses"), strings.HasSuffix(p, "/poddisruptionbudgets"):
			ok(`{"items":[]}`)
		case p == "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			var rv struct {
				Spec struct {
					ResourceAttributes struct{ Resource string } `json:"resourceAttributes"`
				} `json:"spec"`
			}
			_ = json.Unmarshal(body, &rv)
			ok(fmt.Sprintf(`{"status":{"allowed":%t}}`, f.allow[rv.Spec.ResourceAttributes.Resource] || rv.Spec.ResourceAttributes.Resource != "secrets"))
		case r.Method == "PATCH" || r.Method == "DELETE" || r.Method == "POST":
			ok(`{}`)
		default:
			w.WriteHeader(404)
			ok(`{"kind":"Status","message":"not found: ` + p + `"}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// serveForward answers a port-forward like the kubelet: the port first on the data and the error channel, then an upper-case echo.
func (f *fakeCluster) serveForward(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{Subprotocols: []string{"v4.channel.k8s.io"}}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	_ = c.WriteMessage(websocket.BinaryMessage, []byte{0, 0x90, 0x1f})
	_ = c.WriteMessage(websocket.BinaryMessage, []byte{1, 0x90, 0x1f})
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		if msg[0] == 0 {
			_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{0}, strings.ToUpper(string(msg[1:]))...))
		}
	}
}

// serveExec answers a shell like the API server: it echoes what is typed and ends when it reads "exit".
func (f *fakeCluster) serveExec(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{Subprotocols: []string{"v4.channel.k8s.io"}}
	c, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer c.Close()
	for {
		_, msg, err := c.ReadMessage()
		if err != nil {
			return
		}
		if msg[0] != 0 {
			continue
		}
		if strings.TrimSpace(string(msg[1:])) == "exit" {
			_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{3}, `{"status":"Success"}`...))
			return
		}
		_ = c.WriteMessage(websocket.BinaryMessage, append([]byte{1}, msg[1:]...))
	}
}

func (f *fakeCluster) kubeconfig(user string) string {
	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.Certificate().Raw}))
	return fmt.Sprintf(`apiVersion: v1
kind: Config
current-context: prod
clusters:
- name: prod-cluster
  cluster: {server: %s, certificate-authority-data: %s}
contexts:
- name: prod
  context: {cluster: prod-cluster, user: dev}
users:
- name: dev
  user: %s
`, f.URL, ca, user)
}

// clusterSession adds the cluster from pasted text and opens a session on it.
func clusterSession(t *testing.T, h *harness, f *fakeCluster, user string) (sid, clusterID string) {
	t.Helper()
	h.core.Kube = testKube{}
	cl, err := h.app.Cluster.Add(ClusterInput{Label: "prod", Kubeconfig: f.kubeconfig(user)})
	if err != nil {
		t.Fatal(err)
	}
	info, err := h.app.Cluster.Open(cl.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.app.Server.Close(info.SID) })
	return info.SID, cl.ID
}

// ---- reads ----

func TestClusterReadsThroughTheAPIServer(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server

	if d, err := s.K8sDetect(sid); err != nil || !d.Found || !d.Direct {
		t.Fatalf("detect = %+v, %v", d, err)
	}
	cs, err := s.K8sContexts(sid)
	if err != nil || len(cs.Contexts) != 1 || cs.Contexts[0].Name != "prod" || !cs.Contexts[0].Current || cs.Contexts[0].Auth != "token" {
		t.Fatalf("contexts = %+v, %v", cs, err)
	}
	if chk, err := s.K8sCheck(sid, "prod"); err != nil || !chk.OK || chk.ServerVersion != "v1.30.2" || chk.Server != f.URL {
		t.Fatalf("check = %+v, %v", chk, err)
	}
	ns, err := s.K8sNamespaces(sid, "prod")
	if err != nil || len(ns.Names) != 2 || ns.Names[0] != "kube-system" {
		t.Fatalf("namespaces = %+v, %v", ns, err)
	}
	pods, err := s.K8sPods(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if p := podByName(t, pods, "api-7d9f6c-x2kq8"); p.Status != "CrashLoopBackOff" {
		t.Fatalf("pod = %+v", p)
	}
	if got := f.called("GET", "/api/v1/namespaces/shop/pods"); len(got) == 0 {
		t.Fatalf("a namespace is asked for in the path: %v", f.calls)
	}
	if got := f.called("GET", "/api/v1/namespaces/shop/events?fieldSelector=type%3DWarning"); len(got) == 0 {
		t.Fatalf("the field selector is passed on: %v", f.calls)
	}
	if _, err := s.K8sPods(sid, "prod", ""); err != nil { // all namespaces
		t.Fatal(err)
	}
	if got := f.called("GET", "/api/v1/pods"); len(got) == 0 {
		t.Fatalf("all namespaces use the cluster-wide path: %v", f.calls)
	}
	det, err := s.K8sPod(sid, "prod", "shop", "api-7d9f6c-x2kq8")
	if err != nil || det.Pod.Name != "api-7d9f6c-x2kq8" {
		t.Fatalf("pod detail = %+v, %v", det, err)
	}
	w, err := s.K8sWorkloads(sid, "prod", "")
	if err != nil {
		t.Fatal(err)
	}
	if wl := workByName(t, w, "api"); wl.Kind != "Deployment" {
		t.Fatalf("the kind is put on the items of a list: %+v", wl)
	}
	if _, err := s.K8sOverview(sid, "prod"); err != nil {
		t.Fatalf("overview: %v", err)
	}
	nodes, err := s.K8sNodes(sid, "prod") // metrics-server missing: usage is empty, not an error
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, %v", nodes, err)
	}
	if ev, err := s.K8sEvents(sid, "prod", ""); err != nil || len(ev) == 0 {
		t.Fatalf("events = %v, %v", ev, err)
	}
	logs, err := s.K8sLogs(sid, "prod", "shop", "api-7d9f6c-x2kq8", K8sLogOpts{Lines: 300, Since: "15m", Container: "api", Previous: true, Timestamps: true})
	if err != nil || logs != "line one\nline two\n" {
		t.Fatalf("logs = %q, %v", logs, err)
	}
	if got := f.called("GET", "/api/v1/namespaces/shop/pods/api-7d9f6c-x2kq8/log?"); len(got) != 1 ||
		!strings.Contains(got[0], "tailLines=300") || !strings.Contains(got[0], "sinceSeconds=900") || !strings.Contains(got[0], "previous=true") || !strings.Contains(got[0], "container=api") {
		t.Fatalf("log request = %v", got)
	}
}

func TestClusterNeverListsSecretValues(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server

	cfg, err := s.K8sConfig(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	all, _ := json.Marshal(cfg)
	for _, leak := range []string{"hunter2", "s3cret-value", "aHVudGVyMg"} {
		if strings.Contains(string(all), leak) {
			t.Fatalf("the listing carries a value: %s", all)
		}
	}
	y, err := s.K8sYAML(sid, "prod", "secret", "shop", "api-tls")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(y, "hunter2") || strings.Contains(y, "aHVudGVyMg") || strings.Contains(y, "PLAINTEXT-COPY") || strings.Contains(y, "managedFields") || !strings.Contains(y, "password: redacted") {
		t.Fatalf("secret yaml:\n%s", y)
	}
	// one key is revealed only when it is asked for, and the audit log never holds it
	v, err := s.K8sSecretKey(sid, "prod", "shop", "api-tls", "password")
	if err != nil || v != "hunter2" {
		t.Fatalf("key = %q, %v", v, err)
	}
	logs, _ := h.app.Log.List(domainActivityFilter())
	for _, e := range logs {
		if strings.Contains(e.Message, "hunter2") {
			t.Fatalf("the audit log holds the value: %q", e.Message)
		}
	}
	pod, err := s.K8sYAML(sid, "prod", "pod", "shop", "api-7d9f6c-x2kq8")
	if err != nil || strings.Contains(pod, "managedFields") || !strings.Contains(pod, "kind: Pod") && !strings.Contains(pod, "metadata:") {
		t.Fatalf("pod yaml: %v\n%s", err, pod)
	}
}

func TestClusterAccessUsesSelfSubjectAccessReview(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	a, err := h.app.Server.K8sAccess(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Rights["list"] || !a.Rights["scale"] || a.Rights["secrets"] {
		t.Fatalf("rights = %v", a.Rights)
	}
	reviews := f.called("POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews")
	if want := len(k8sRightSpec) + len(websocketVerbs); len(reviews) != want { // the shell and the port-forward are asked with `get` too
		t.Fatalf("asked %d questions, want %d", len(reviews), want)
	}
	// scaling is a patch of the Deployment here, so that is the question: never the separate `deployments/scale`
	var patches int
	for _, r := range reviews {
		if strings.Contains(r, `"subresource":"scale"`) {
			t.Fatalf("asked about the scale subresource: %s", r)
		}
		if strings.Contains(r, `"resource":"deployments"`) && strings.Contains(r, `"verb":"patch"`) && strings.Contains(r, `"group":"apps"`) && strings.Contains(r, `"namespace":"shop"`) {
			patches++
		}
	}
	if patches != 2 { // scale and restart
		t.Fatalf("patch-deployments questions = %d", patches)
	}
}

func TestClusterReportsWhyItCannotConnect(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: wrong}")
	chk, err := h.app.Server.K8sCheck(sid, "prod")
	if err != nil || chk.OK || chk.Kind != "unauthorized" {
		t.Fatalf("check = %+v, %v", chk, err)
	}
	if _, err := h.app.Server.K8sNamespaces(sid, "prod"); err == nil {
		t.Fatal("a read with a wrong token must fail")
	}
}

// ---- changes ----

func TestClusterChangesAreRequestsAndAreAudited(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server

	if err := s.K8sScale(sid, "prod", "shop", "api", 5); err != nil {
		t.Fatal(err)
	}
	if got := f.called("PATCH", "/apis/apps/v1/namespaces/shop/deployments/api "); len(got) != 1 || !strings.Contains(got[0], `"replicas":5`) {
		t.Fatalf("scale patches the Deployment: %v", got)
	}
	if got := f.called("PATCH", "/apis/apps/v1/namespaces/shop/deployments/api/scale"); len(got) != 0 {
		t.Fatalf("the scale subresource is not used: %v", got)
	}
	if err := s.K8sRolloutPause(sid, "prod", "shop", "api", true); err != nil {
		t.Fatal(err)
	}
	paused := 0
	for _, c := range f.called("PATCH", "/apis/apps/v1/namespaces/shop/deployments/api ") { // the scale above went to the same path
		if strings.Contains(c, `"paused":true`) {
			paused++
		}
	}
	if paused != 1 {
		t.Fatalf("pause = %v", f.calls)
	}
	if err := s.K8sDeletePod(sid, "prod", "shop", "api-7d9f6c-x2kq8"); err != nil {
		t.Fatal(err)
	}
	if len(f.called("DELETE", "/api/v1/namespaces/shop/pods/api-7d9f6c-x2kq8")) != 1 {
		t.Fatalf("delete = %v", f.calls)
	}
	if err := s.K8sCordon(sid, "prod", "worker-2", true); err != nil {
		t.Fatal(err)
	}
	if got := f.called("PATCH", "/api/v1/nodes/worker-2"); len(got) != 1 || !strings.Contains(got[0], `"unschedulable":true`) {
		t.Fatalf("cordon = %v", got)
	}
	if err := s.K8sCronJob(sid, "prod", "shop", "nightly", "suspend"); err != nil {
		t.Fatal(err)
	}
	if got := f.called("PATCH", "/apis/batch/v1/namespaces/shop/cronjobs/nightly"); len(got) != 1 || !strings.Contains(got[0], `"suspend":true`) {
		t.Fatalf("suspend = %v", got)
	}
	logs, _ := h.app.Log.List(domainActivityFilter())
	var text []string
	for _, e := range logs {
		text = append(text, e.Message)
	}
	joined := strings.Join(text, "\n")
	for _, want := range []string{"scaled deployment api to 5", "deleted pod api-7d9f6c-x2kq8", "cordoned node worker-2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the audit log lacks %q:\n%s", want, joined)
		}
	}
}

func TestClusterRestartWaitsForTheRollout(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	d := h.app.Server.kubeOf(sid)
	// the fake answers the Deployment as already rolled out (replicas 2, all updated and available)
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"metadata":{"generation":3},"spec":{"replicas":2},"status":{"observedGeneration":3,"replicas":2,"updatedReplicas":2,"availableReplicas":2}}`))
	})
	if err := d.restart("prod", "shop", "deployment", "api", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	got := f.called("PATCH", "/apis/apps/v1/namespaces/shop/deployments/api")
	if len(got) != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
	// a rollout that never finishes ends with a message that says so
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"metadata":{"generation":4},"spec":{"replicas":2},"status":{"observedGeneration":3,"replicas":2,"updatedReplicas":1}}`))
	})
	if err := d.restart("prod", "shop", "deployment", "api", 10*time.Millisecond); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestClusterRollbackReplacesTheTemplateOfThatRevision(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		f.mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, "/replicasets"):
			_, _ = w.Write([]byte(`{"items":[
			 {"metadata":{"annotations":{"deployment.kubernetes.io/revision":"2"},"ownerReferences":[{"kind":"Deployment","name":"api"}]},"spec":{"template":{"metadata":{"labels":{"app":"api","pod-template-hash":"abc"}},"spec":{"containers":[{"name":"api","image":"api:1.7"}]}}}},
			 {"metadata":{"annotations":{"deployment.kubernetes.io/revision":"3"},"ownerReferences":[{"kind":"Deployment","name":"api"}]},"spec":{"template":{"spec":{"containers":[{"name":"api","image":"api:1.8"}]}}}}]}`))
		case r.Method == "GET":
			_, _ = w.Write([]byte(`{"spec":{"selector":{"matchLabels":{"app":"api"}}}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	if err := h.app.Server.K8sRollback(sid, "prod", "shop", "api", 2); err != nil {
		t.Fatal(err)
	}
	got := f.called("PATCH", "/apis/apps/v1/namespaces/shop/deployments/api")
	if len(got) != 1 || !strings.Contains(got[0], `"op":"replace"`) || !strings.Contains(got[0], "api:1.7") || strings.Contains(got[0], "pod-template-hash") {
		t.Fatalf("rollback = %v", got)
	}
	if err := h.app.Server.K8sRollback(sid, "prod", "shop", "api", 9); err == nil || !strings.Contains(err.Error(), "unable to find") {
		t.Fatalf("a revision that is not there: %v", err)
	}
}

func TestClusterDrainRefusesBeforeEvictingAnything(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		if r.Method == "GET" && r.URL.Path == "/api/v1/pods" {
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"bare","namespace":"shop"},"spec":{},"status":{"phase":"Running"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	err := h.app.Server.K8sDrain(sid, "prod", "worker-2", K8sDrainOpts{IgnoreDaemonsets: true})
	if err == nil || !strings.Contains(err.Error(), "shop/bare has no controller") {
		t.Fatalf("err = %v", err)
	}
	if len(f.called("POST", "/api/v1/namespaces/shop/pods/bare/eviction")) != 0 {
		t.Fatal("nothing may be evicted when the drain is refused")
	}
}

func TestClusterDrainEvictsAndWaitsForThePodsToGo(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	var gone bool
	var mu sync.Mutex
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/pods":
			_, _ = w.Write([]byte(`{"items":[{"metadata":{"name":"web-1","namespace":"shop","uid":"u1","ownerReferences":[{"kind":"ReplicaSet","name":"web"}]},"spec":{},"status":{"phase":"Running"}}]}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/eviction"):
			gone = true
			_, _ = w.Write([]byte(`{}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/namespaces/shop/pods/web-1":
			if gone {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"message":"not found"}`))
				return
			}
			_, _ = w.Write([]byte(`{"metadata":{"uid":"u1"}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})
	if err := h.app.Server.K8sDrain(sid, "prod", "worker-2", K8sDrainOpts{TimeoutSec: 30}); err != nil {
		t.Fatal(err)
	}
	if len(f.called("PATCH", "/api/v1/nodes/worker-2")) != 1 || len(f.called("POST", "/api/v1/namespaces/shop/pods/web-1/eviction")) != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestClusterRunsCronJobNowFromItsTemplate(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI()+" "+string(body))
		f.mu.Unlock()
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{"metadata":{"uid":"cj-uid"},"spec":{"jobTemplate":{"metadata":{"labels":{"app":"sync"}},"spec":{"template":{"spec":{"containers":[{"name":"c","image":"sync"}]}}}}}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := h.app.Server.K8sCronJob(sid, "prod", "data", "cron-sync", "run"); err != nil {
		t.Fatal(err)
	}
	got := f.called("POST", "/apis/batch/v1/namespaces/data/jobs")
	if len(got) != 1 || !strings.Contains(got[0], `"cronjob.kubernetes.io/instantiate":"manual"`) || !strings.Contains(got[0], `"uid":"cj-uid"`) || !strings.Contains(got[0], "cron-sync-manual-") {
		t.Fatalf("job = %v", got)
	}
}

// ---- what a kubeconfig cannot do ----

func TestClusterHasNoKubectlShellLineAndNoHostOnlyTab(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	if _, err := h.app.Server.K8sShellCommand(sid, "prod", "shop", "p", ""); err == nil || !strings.Contains(err.Error(), "kubectl") {
		t.Fatalf("shell: %v", err)
	}
	if _, err := h.app.Server.Overview(sid); err == nil {
		t.Fatal("a host-only tab must not run anything for a cluster")
	}
}

// askPort sends text to a local port and reads the answer of the same length.
func askPort(t *testing.T, port int, text string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(text))
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatalf("no answer: %v", err)
	}
	return string(buf)
}

func TestClusterPortForwardGoesThroughTheAPIServerToAReadyPod(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	local := freePort(t)
	fw, err := s.K8sPortForwardStart(sid, "prod", "shop", "web-svc", local, 80)
	if err != nil || fw.Status != "running" || fw.LocalPort != local {
		t.Fatalf("%+v %v", fw, err)
	}
	// the Service port 80 is the container port called http (8080) of the pod that is ready: not the one that is not ready, not the one that is going away
	if got := askPort(t, local, "hello"); got != "HELLO" {
		t.Fatalf("%q", got)
	}
	if got := askPort(t, local, "again"); got != "AGAIN" { // each connection is a call of its own
		t.Fatalf("%q", got)
	}
	if c := f.called("GET", "/api/v1/namespaces/shop/pods/web-1/portforward?ports=8080"); len(c) < 3 { // the first connection that proves the pod, and two more
		t.Fatalf("%v", f.calls)
	}
	if c := f.called("GET", "/api/v1/namespaces/shop/pods/web-0/portforward"); len(c) != 0 {
		t.Fatalf("a pod that is not ready was used: %v", c)
	}
	list, err := s.K8sPortForwards(sid)
	if err != nil || len(list) != 1 || list[0].Service != "web-svc" {
		t.Fatalf("%+v %v", list, err)
	}
	if err := s.K8sPortForwardStop(sid, fw.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(local), time.Second); err == nil {
		t.Fatal("the local port still answers after the stop")
	}
	if l, _ := s.K8sPortForwards(sid); len(l) != 1 || l[0].Status != "stopped" {
		t.Fatalf("%+v", l)
	}
	var started, stopped int
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.Contains(e.Message, "started port-forward svc/web-svc:80 on 127.0.0.1:"+strconv.Itoa(local)) && strings.Contains(e.Message, "pod web-1") {
			started++
		}
		if strings.Contains(e.Message, "stopped port-forward svc/web-svc") {
			stopped++
		}
	}
	if started != 1 || stopped != 1 {
		t.Fatalf("audit: started %d, stopped %d", started, stopped)
	}
}

func TestClusterPortForwardPortsAndRefusals(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	for port, want := range map[int]string{81: "ports=8081", 90: "ports=90"} { // a number as it is; no targetPort is the Service port itself
		local := freePort(t)
		fw, err := s.K8sPortForwardStart(sid, "prod", "shop", "web-svc", local, port)
		if err != nil {
			t.Fatalf("%d: %v", port, err)
		}
		if got := askPort(t, local, "x"); got != "X" {
			t.Fatalf("%q", got)
		}
		if c := f.called("GET", "/api/v1/namespaces/shop/pods/web-1/portforward?"+want); len(c) == 0 {
			t.Fatalf("%d: %v", port, f.calls)
		}
		_ = s.K8sPortForwardStop(sid, fw.ID)
	}
	for _, c := range []struct{ svc, want string }{{"web-svc", "has no port 99"}, {"headless", "no selector"}, {"lonely", "running and ready"}} {
		local := freePort(t)
		if _, err := s.K8sPortForwardStart(sid, "prod", "shop", c.svc, local, map[bool]int{true: 99, false: 80}[c.svc == "web-svc"]); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.svc, err)
		}
		if l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(local)); err != nil {
			t.Errorf("%s: the local port was left open after a refusal", c.svc)
		} else {
			_ = l.Close()
		}
	}
}

func TestClusterDebugContainerIsAddedAndWaitedFor(t *testing.T) {
	k8sDebugPoll = 5 * time.Millisecond
	defer func() { k8sDebugPoll = time.Second }()
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	d, err := s.K8sDebugStart(sid, "prod", "shop", "dbg", "nicolaka/netshoot", "app")
	if err != nil || !strings.HasPrefix(d.Container, "vutty-debug-") || d.Command != "" {
		t.Fatalf("%+v %v", d, err)
	}
	c := f.called("PATCH", "/api/v1/namespaces/shop/pods/dbg/ephemeralcontainers")
	if len(c) != 1 || !strings.Contains(c[0], `"image":"nicolaka/netshoot"`) || !strings.Contains(c[0], `"targetContainerName":"app"`) || !strings.Contains(c[0], `"tty":true`) || !strings.Contains(c[0], d.Container) {
		t.Fatalf("%v", c)
	}
	// an image that cannot be pulled is said at once, not after the wait
	if _, err := s.K8sDebugStart(sid, "prod", "shop", "dbgbad", "nope/none", ""); err == nil || !strings.Contains(err.Error(), "ImagePullBackOff") || !strings.Contains(err.Error(), "pull access denied") {
		t.Fatalf("%v", err)
	}
	if c := f.called("PATCH", "/api/v1/namespaces/shop/pods/dbgbad/ephemeralcontainers"); len(c) != 1 || strings.Contains(c[0], "targetContainerName") {
		t.Fatalf("no target means none is sent: %v", c)
	}
	var ok, bad int
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.Contains(e.Message, "added the debug container "+d.Container+" (nicolaka/netshoot) to pod dbg in shop (prod)") {
			ok++
		}
		if strings.Contains(e.Message, "debug container (nope/none) for pod dbgbad") {
			bad++
		}
	}
	if ok != 1 || bad != 1 {
		t.Fatalf("audit: %d %d", ok, bad)
	}
	// a name that is not an image, a pod that is not a name
	for _, img := range []string{"", "a b", "x;rm", "a/../b", "-x", "'q'"} {
		if _, err := s.K8sDebugStart(sid, "prod", "shop", "dbg", img, ""); err == nil {
			t.Errorf("image %q", img)
		}
	}
	for _, bad := range k8sBadWords {
		if _, err := s.K8sDebugStart(sid, "prod", "shop", bad, "busybox", ""); err == nil {
			t.Errorf("pod %q", bad)
		}
		if _, err := s.K8sDebugStart(sid, "prod", "shop", "dbg", "busybox", bad); err == nil && bad != "" {
			t.Errorf("target %q", bad)
		}
	}
}

func TestClusterDebugContainerGivesUpWhenItNeverRuns(t *testing.T) {
	k8sDebugPoll, k8sDebugWait = 5*time.Millisecond, 40*time.Millisecond
	defer func() { k8sDebugPoll, k8sDebugWait = time.Second, 90*time.Second }()
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	if _, err := h.app.Server.K8sDebugStart(sid, "prod", "shop", "api-7d9f6c-x2kq8", "busybox", ""); err == nil || !strings.Contains(err.Error(), "did not start in time") {
		t.Fatalf("%v", err)
	}
}

func TestHostDebugContainerIsTheLineKubectlDebugTypesInATab(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) { return "", "", 0 })
	d, err := h.app.Server.K8sDebugStart(sid, "prod", "shop", "api-1", "nicolaka/netshoot", "app")
	if err != nil || d.Container != "" || !strings.HasPrefix(d.Command, "kubectl debug -it -n shop api-1 --image=nicolaka/netshoot --target=app --context prod -- sh -c ") {
		t.Fatalf("%+v %v", d, err)
	}
	if d, _ := h.app.Server.K8sDebugStart(sid, "prod", "shop", "api-1", "busybox", ""); strings.Contains(d.Command, "--target") {
		t.Fatalf("%q", d.Command)
	}
}

// ---- the record, the vault and the login program ----

func TestClusterKubeconfigStaysInTheVault(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	h.core.Kube = testKube{}
	cl, err := h.app.Cluster.Add(ClusterInput{Label: "prod", Kubeconfig: f.kubeconfig("{token: super-secret-token}")})
	if err != nil {
		t.Fatal(err)
	}
	for name, v := range map[string]any{"record": cl, "list": h.app.Cluster.List(), "snapshot": h.core.Store.Snapshot()} {
		b, _ := json.Marshal(v)
		if strings.Contains(string(b), "super-secret-token") || strings.Contains(string(b), "certificate-authority-data") {
			t.Fatalf("the %s holds the kubeconfig: %s", name, b)
		}
	}
	pv, err := h.app.Cluster.Describe(cl.ID)
	if b, _ := json.Marshal(pv); err != nil || strings.Contains(string(b), "super-secret-token") {
		t.Fatalf("describe leaks: %s %v", b, err)
	}
	if cl.Context != "prod" || cl.Server != f.URL || cl.Label != "prod" {
		t.Fatalf("cluster = %+v", cl)
	}
	// deleting the cluster deletes the secret
	if err := h.app.Cluster.Delete(cl.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.core.Vault.GetSecret(cl.SecretRef); err == nil {
		t.Fatal("the kubeconfig outlived the cluster")
	}
	if len(h.app.Cluster.List()) != 0 {
		t.Fatal("still listed")
	}
}

func TestClusterRefusesKubeconfigsThatCouldLeakAFile(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	h.core.Kube = testKube{}
	for name, user := range map[string]string{"tokenFile": "{tokenFile: /Users/me/.ssh/id_rsa}", "auth-provider": "{auth-provider: {name: gcp}}"} {
		if _, err := h.app.Cluster.Add(ClusterInput{Kubeconfig: f.kubeconfig(user)}); err == nil {
			t.Errorf("%s: a kubeconfig that reads a local file must be refused", name)
		}
	}
	if _, err := h.app.Cluster.Add(ClusterInput{Kubeconfig: "not: [a kubeconfig"}); err == nil {
		t.Error("garbage must be refused")
	}
	if len(h.app.Cluster.List()) != 0 {
		t.Fatal("a refused kubeconfig must not leave a record")
	}
}

func TestClusterLoginProgramMustBeApprovedAndTheScreenCannotApproveAnother(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the login program is a shell script")
	}
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "get-token")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntouch "+marker+"\necho '{\"status\":{\"token\":\"tok\"}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sid, id := clusterSession(t, h, f, "{exec: {apiVersion: client.authentication.k8s.io/v1, command: "+script+"}}")
	chk, err := h.app.Server.K8sCheck(sid, "prod")
	if err != nil || chk.OK || chk.Kind != "approval" || chk.Raw != script {
		t.Fatalf("check = %+v, %v", chk, err)
	}
	if _, serr := os.Stat(marker); serr == nil {
		t.Fatal("the program ran before it was approved")
	}
	if _, err := h.app.Cluster.ApproveExec(id, "/bin/rm -rf /"); err == nil {
		t.Fatal("a command the kubeconfig does not name must not be approvable")
	}
	cl, err := h.app.Cluster.ApproveExec(id, script)
	if err != nil || len(cl.ApprovedExec) != 1 {
		t.Fatalf("approve: %+v %v", cl, err)
	}
	// the session that asked can repeat the check at once
	if chk, err := h.app.Server.K8sCheck(sid, "prod"); err != nil || !chk.OK {
		t.Fatalf("same session after approval: %+v, %v", chk, err)
	}
	// a new session knows the approval (it is kept with the cluster)
	info, err := h.app.Cluster.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	defer h.app.Server.Close(info.SID)
	if chk, err := h.app.Server.K8sCheck(info.SID, "prod"); err != nil || !chk.OK {
		t.Fatalf("after approval: %+v, %v", chk, err)
	}
	if _, serr := os.Stat(marker); serr != nil {
		t.Fatal("the approved program did not run")
	}
}

func TestClusterRevokingALoginProgramAsksAgainAtTheNextConnection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the login program is a shell script")
	}
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "get-token")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho '{\"status\":{\"token\":\"tok\"}}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sid, id := clusterSession(t, h, f, "{exec: {apiVersion: client.authentication.k8s.io/v1, command: "+script+"}}")
	if _, err := h.app.Cluster.ApproveExec(id, script); err != nil {
		t.Fatal(err)
	}
	if chk, err := h.app.Server.K8sCheck(sid, "prod"); err != nil || !chk.OK {
		t.Fatalf("approved: %+v %v", chk, err)
	}
	cl, err := h.app.Cluster.RevokeExec(id, script)
	if err != nil || len(cl.ApprovedExec) != 0 {
		t.Fatalf("revoke = %+v, %v", cl, err)
	}
	if chk, err := h.app.Server.K8sCheck(sid, "prod"); err != nil || chk.OK || chk.Kind != "approval" {
		t.Fatalf("the open session asks again: %+v %v", chk, err)
	}
	info, err := h.app.Cluster.Open(id)
	if err != nil {
		t.Fatal(err)
	}
	defer h.app.Server.Close(info.SID)
	if chk, _ := h.app.Server.K8sCheck(info.SID, "prod"); chk.Kind != "approval" {
		t.Fatalf("a new session asks too: %+v", chk)
	}
	if again, err := h.app.Cluster.RevokeExec(id, script); err != nil || len(again.ApprovedExec) != 0 {
		t.Fatalf("revoking what is not approved is not an error: %+v %v", again, err)
	}
	if !strings.Contains(auditText(h), "Login program no longer approved") {
		t.Fatalf("audit:\n%s", auditText(h))
	}
}

func TestClusterReplaceKubeconfigDropsApprovals(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	_, id := clusterSession(t, h, f, "{token: tok}")
	cur, _ := h.core.Store.GetCluster(id)
	cur.ApprovedExec = []string{"aws eks get-token"}
	if _, err := h.core.Store.SaveCluster(cur); err != nil {
		t.Fatal(err)
	}
	cl, err := h.app.Cluster.ReplaceKubeconfig(id, "", f.kubeconfig("{token: tok2}"))
	if err != nil || len(cl.ApprovedExec) != 0 {
		t.Fatalf("replace = %+v, %v", cl, err)
	}
}

func TestClustersAreTickedApartFromKeysAndTheirKubeconfigsGoWithThem(t *testing.T) {
	d := domainDataWithCluster()
	d.Identities = []domain.Identity{{Name: "k"}}
	secrets := []domain.Secret{{Kind: domain.SecretKubeconfig}, {Kind: domain.SecretPassword}, {Kind: domain.SecretPrivateKey}}
	kinds := func(ss []domain.Secret) string {
		var k []string
		for _, s := range ss {
			k = append(k, string(s.Kind))
		}
		return strings.Join(k, ",")
	}
	out, sec := selectData(d, secrets, []string{syncHosts})
	if len(out.Clusters) != 0 || len(out.Identities) != 0 || len(sec) != 0 {
		t.Fatalf("nothing ticked, nothing of either goes: %+v %v", out, kinds(sec))
	}
	out, sec = selectData(d, secrets, []string{syncClusters})
	if len(out.Clusters) != 1 || len(out.Identities) != 0 || kinds(sec) != "kubeconfig" {
		t.Fatalf("clusters ticked: the clusters and only their kubeconfigs go: %+v %v", out, kinds(sec))
	}
	out, sec = selectData(d, secrets, []string{syncKeys})
	if len(out.Clusters) != 0 || len(out.Identities) != 1 || kinds(sec) != "password,private_key" {
		t.Fatalf("keys ticked: the keys and their secrets go, no kubeconfig: %+v %v", out, kinds(sec))
	}
	// what is kept after a sync: a ticked kind is the merged one, an unticked one stays as it was, secrets with it
	own := domain.Data{Clusters: []domain.Cluster{{Label: "mine"}}}
	ownSecrets := []domain.Secret{{Kind: domain.SecretKubeconfig, Meta: domain.Meta{ID: "own-kc"}}, {Kind: domain.SecretPassword, Meta: domain.Meta{ID: "own-pw"}}}
	merged := domain.Data{Clusters: []domain.Cluster{{Label: "theirs"}}}
	mergedSecrets := []domain.Secret{{Kind: domain.SecretKubeconfig, Meta: domain.Meta{ID: "m-kc"}}, {Kind: domain.SecretPassword, Meta: domain.Meta{ID: "m-pw"}}}
	ids := func(ss []domain.Secret) string {
		var k []string
		for _, s := range ss {
			k = append(k, s.ID)
		}
		sort.Strings(k)
		return strings.Join(k, ",")
	}
	kept, keptSecrets := keepUnticked(merged, own, mergedSecrets, ownSecrets, []string{syncKeys})
	if kept.Clusters[0].Label != "mine" || ids(keptSecrets) != "m-pw,own-kc" {
		t.Fatalf("keys ticked, clusters not: %+v %s", kept.Clusters, ids(keptSecrets))
	}
	kept, keptSecrets = keepUnticked(merged, own, mergedSecrets, ownSecrets, []string{syncClusters})
	if kept.Clusters[0].Label != "theirs" || ids(keptSecrets) != "m-kc,own-pw" {
		t.Fatalf("clusters ticked, keys not: %+v %s", kept.Clusters, ids(keptSecrets))
	}
}

func TestAClusterSyncsToAnotherMachineWithItsKubeconfigAndCanBeLeftOut(t *testing.T) {
	remote := bareRepo(t)
	f := newFakeCluster(t)
	a := newHarness(t).unlocked(t)
	a.core.Kube = testKube{}
	cl, err := a.app.Cluster.Add(ClusterInput{Label: "prod", Kubeconfig: f.kubeconfig("{token: tok}")})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	b := newHarness(t)
	b.core.Kube = testKube{}
	if err := b.app.Sync.Configure(remote); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.JoinExisting(masterPW); err != nil {
		t.Fatal(err)
	}
	got := b.app.Cluster.List()
	if len(got) != 1 || got[0].ID != cl.ID || got[0].Label != "prod" {
		t.Fatalf("the cluster is on the other machine: %+v", got)
	}
	info, err := b.app.Cluster.Open(cl.ID) // with its kubeconfig, which came with it
	if err != nil {
		t.Fatalf("the kubeconfig did not come with the cluster: %v", err)
	}
	defer b.app.Server.Close(info.SID)
	if chk, err := b.app.Server.K8sCheck(info.SID, "prod"); err != nil || !chk.OK {
		t.Fatalf("check on B = %+v, %v", chk, err)
	}

	// B leaves the clusters out: a new cluster of A is not taken, one of B stays and is not sent
	setPrefs(t, b, func(s *Settings) { s.SyncWhat = []string{syncKeys, syncHosts} })
	if _, err := a.app.Cluster.Add(ClusterInput{Label: "from A", Kubeconfig: f.kubeconfig("{token: tok}")}); err != nil {
		t.Fatal(err)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.app.Cluster.Add(ClusterInput{Label: "only on B", Kubeconfig: f.kubeconfig("{token: tok}")}); err != nil {
		t.Fatal(err)
	}
	if err := b.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	var onB []string
	for _, c := range b.app.Cluster.List() {
		onB = append(onB, c.Label)
	}
	sort.Strings(onB)
	if strings.Join(onB, ",") != "only on B,prod" {
		t.Fatalf("what is not ticked is not taken, and stays: %v", onB)
	}
	if err := a.app.Sync.SyncNow(); err != nil {
		t.Fatal(err)
	}
	for _, c := range a.app.Cluster.List() {
		if c.Label == "only on B" {
			t.Fatal("what is not ticked is not sent")
		}
	}
}

func domainActivityFilter() domain.ActivityFilter { return domain.ActivityFilter{} }

func domainDataWithCluster() domain.Data {
	return domain.Data{Clusters: []domain.Cluster{{Label: "x"}}}
}

// A login that may only look inside its own namespace: the Overview of that namespace works, and says the nodes are hidden.
func TestOverviewInANamespaceWithoutRightToListNodes(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	inner := f.Server.Config.Handler
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/nodes" || r.URL.Path == "/api/v1/pods" { // cluster-wide lists are refused
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"kind":"Status","message":"` + strings.TrimPrefix(r.URL.Path, "/api/v1/") + ` is forbidden: cannot list at the cluster scope"}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	if _, err := h.app.Server.K8sOverviewIn(sid, "prod", ""); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("all namespaces must say why it cannot: %v", err)
	}
	o, err := h.app.Server.K8sOverviewIn(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if !o.NodesHidden || o.Pods.Total == 0 || o.Nodes.Total != 0 {
		t.Fatalf("overview = %+v", o)
	}
	if len(f.called("GET", "/api/v1/namespaces/shop/pods")) == 0 {
		t.Fatalf("the namespace is asked for in the path: %v", f.calls)
	}
}

// ---- a shell in a pod, over the API's WebSocket ----

func TestClusterShellInAPodIsATerminalTab(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	sh, err := h.app.Server.K8sShellSession(sid, "prod", "shop", "api-7d9f6c-x2kq8", "api")
	if err != nil || !strings.HasPrefix(sh.HostID, kubeExecPrefix) || !strings.Contains(sh.Title, "api-7d9f6c-x2kq8") {
		t.Fatalf("shell = %+v, %v", sh, err)
	}
	if len(f.called("GET", "/api/v1/namespaces/shop/pods/api-7d9f6c-x2kq8/exec")) != 0 {
		t.Fatal("nothing runs until the terminal tab opens")
	}
	tid, err := h.app.Session.Open(sh.HostID, 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	h.rec.wait(t, "term:ready:"+tid)
	if err := h.app.Session.Attach(tid); err != nil { // the screen subscribes, then output flows
		t.Fatal(err)
	}
	if err := h.app.Session.Write(tid, "hello\n"); err != nil {
		t.Fatal(err)
	}
	h.rec.waitOutput(t, tid, "hello")
	if err := h.app.Session.Resize(tid, 120, 40); err != nil {
		t.Fatal(err)
	}
	exec := f.called("GET", "/api/v1/namespaces/shop/pods/api-7d9f6c-x2kq8/exec")
	if len(exec) != 1 || !strings.Contains(exec[0], "container=api") || !strings.Contains(exec[0], "tty=true") || !strings.Contains(exec[0], "exec+bash") && !strings.Contains(exec[0], "exec%20bash") {
		t.Fatalf("exec request = %v", exec)
	}
	logs, _ := h.app.Log.List(domainActivityFilter())
	var text []string
	for _, e := range logs {
		text = append(text, e.Message)
	}
	if j := strings.Join(text, "\n"); !strings.Contains(j, "shell in pod api-7d9f6c-x2kq8") || !strings.Contains(j, "Opened a shell in") {
		t.Fatalf("the audit log:\n%s", j)
	}
	if err := h.app.Session.Write(tid, "exit\n"); err != nil {
		t.Fatal(err)
	}
	h.rec.wait(t, "term:closed:"+tid)
}

func TestClusterShellRefusesWhatIsNotAPodOfACluster(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	for _, bad := range k8sBadWords {
		if _, err := h.app.Server.K8sShellSession(sid, "prod", "shop", bad, ""); err == nil {
			t.Errorf("pod %q accepted", bad)
		}
		if _, err := h.app.Server.K8sShellSession(sid, "prod", "shop", "p", bad); err == nil {
			t.Errorf("container %q accepted", bad)
		}
	}
	if _, err := h.app.Session.Open(kubeExecPrefix+"does-not-exist", 80, 24); err == nil {
		t.Fatal("a terminal for a pod nobody prepared must be refused")
	}
	if _, err := h.app.Server.K8sShellSession("no-such-session", "prod", "shop", "p", ""); err == nil {
		t.Fatal("no session")
	}
}

func TestClusterShellNeedsTheClusterTabToStayOpenToReconnect(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	sh, err := h.app.Server.K8sShellSession(sid, "prod", "shop", "api-7d9f6c-x2kq8", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.Close(sid); err != nil { // the Kubernetes tab is closed: the kubeconfig leaves memory
		t.Fatal(err)
	}
	tid, err := h.app.Session.Open(sh.HostID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	ev := h.rec.wait(t, "term:closed:"+tid)
	if got := fmt.Sprint(ev); !strings.Contains(got, "open the cluster again") {
		t.Fatalf("closed = %s", got)
	}
}

// ---- a list of namespaces, for a login that may not list the cluster ----

func TestNamespaceListsAreOnlyForLists(t *testing.T) {
	for _, ok := range []string{"", "shop", "a,b", "a,b,c"} {
		if err := k8sNS(ok, true); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"a,", ",a", "a,,b", "a, b", "a;b", strings.Repeat("a,", 31) + "a"} {
		if err := k8sNS(bad, true); err == nil {
			t.Errorf("list %q accepted", bad)
		}
	}
	for _, one := range []string{"a,b", "a,"} {
		if err := k8sNS(one, false); err == nil { // a call about one object takes one name
			t.Errorf("%q accepted for one object", one)
		}
	}
}

func TestClusterListsPutTheNamespacesTogetherAndLeaveOutOneThatIsRefused(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	inner := f.Server.Config.Handler
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/namespaces/denied") {
			f.mu.Lock()
			f.calls = append(f.calls, r.Method+" "+r.URL.RequestURI())
			f.mu.Unlock()
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"kind":"Status","message":"pods is forbidden: cannot list in the namespace denied"}`))
			return
		}
		inner.ServeHTTP(w, r)
	})
	one, err := h.app.Server.K8sPods(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	both, err := h.app.Server.K8sPods(sid, "prod", "shop,other,denied")
	if err != nil {
		t.Fatalf("one namespace that is refused must not hide the others: %v", err)
	}
	if len(both) != 2*len(one) {
		t.Fatalf("pods: %d for one namespace, %d for two", len(one), len(both))
	}
	for _, ns := range []string{"shop", "other", "denied"} {
		if len(f.called("GET", "/api/v1/namespaces/"+ns+"/pods")) == 0 {
			t.Errorf("namespace %s was not asked: %v", ns, f.calls)
		}
	}
	if _, err := h.app.Server.K8sPods(sid, "prod", "denied,denied2"); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("when every namespace is refused the reason is said: %v", err)
	}
	if _, err := h.app.Server.K8sWorkloads(sid, "prod", "shop,other"); err != nil {
		t.Fatalf("workloads: %v", err)
	}
	o, err := h.app.Server.K8sOverviewIn(sid, "prod", "shop,other")
	if err != nil || o.Pods.Total != 2*len(one) {
		t.Fatalf("overview: %+v %v", o, err)
	}
	if _, err := h.app.Server.K8sAccess(sid, "prod", "shop,other"); err == nil {
		t.Fatal("rights are asked for one namespace")
	}
	if err := h.app.Server.K8sScale(sid, "prod", "shop,other", "api", 2); err == nil {
		t.Fatal("a change takes one namespace")
	}
}

// A login that "may exec" in Rancher has `get` on pods/exec, not `create`: the API server authorizes a WebSocket as a GET (and, from
// Kubernetes 1.35, wants `create` too). Asking for `create` alone locked the Shell of a login that can open one.
func TestClusterShellRightIsTheOneAWebSocketNeeds(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	inner := f.Server.Config.Handler
	var mu sync.Mutex
	minor, getOK, createOK := "34+", true, false
	f.Server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/version":
			_, _ = w.Write([]byte(`{"major":"1","minor":"` + minor + `","gitVersion":"v1.` + strings.TrimRight(minor, "+") + `.0"}`))
		case r.URL.Path == "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews":
			body, _ := io.ReadAll(r.Body)
			var rv struct {
				Spec struct {
					ResourceAttributes struct{ Verb, Resource, Subresource string } `json:"resourceAttributes"`
				} `json:"spec"`
			}
			_ = json.Unmarshal(body, &rv)
			a := rv.Spec.ResourceAttributes
			allowed := true
			if a.Resource == "pods" && a.Subresource == "exec" {
				allowed = a.Verb == "get" && getOK || a.Verb == "create" && createOK
			}
			_, _ = w.Write([]byte(fmt.Sprintf(`{"status":{"allowed":%t}}`, allowed)))
		default:
			inner.ServeHTTP(w, r)
		}
	})
	exec := func() bool {
		a, err := h.app.Server.K8sAccess(sid, "prod", "shop")
		if err != nil {
			t.Fatal(err)
		}
		return a.Rights["exec"]
	}
	if !exec() { // `get` only, on a server before 1.35: the Rancher case
		t.Fatal("a login with `get` on pods/exec may open a shell on Kubernetes 1.34")
	}
	mu.Lock()
	minor = "35"
	mu.Unlock()
	if exec() {
		t.Fatal("from 1.35 a WebSocket exec also needs `create`")
	}
	mu.Lock()
	createOK = true
	mu.Unlock()
	if !exec() {
		t.Fatal("with `get` and `create` the shell is open on every version")
	}
	mu.Lock()
	getOK, minor = false, "30"
	mu.Unlock()
	if exec() {
		t.Fatal("`create` alone is not what a WebSocket needs")
	}
}

// A pod of several containers, asked without naming one: kubectl takes the one the pod names as its default, else the first. The
// API server alone refuses ("a container name must be specified ... choose one of"), which is what the Logs screen showed.
func TestClusterPodOfSeveralContainersUsesTheDefaultOneLikeKubectl(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	got, err := s.K8sLogs(sid, "prod", "shop", "multi", K8sLogOpts{Lines: 100})
	if err != nil || got != "log of aws-ssm\n" {
		t.Fatalf("without a name the first container is used: %q %v", got, err)
	}
	got, err = s.K8sLogs(sid, "prod", "shop", "tagged", K8sLogOpts{Lines: 100})
	if err != nil || got != "log of nginx\n" {
		t.Fatalf("the default-container annotation wins: %q %v", got, err)
	}
	got, err = s.K8sLogs(sid, "prod", "shop", "multi", K8sLogOpts{Lines: 100, Container: "nginx"})
	if err != nil || got != "log of nginx\n" {
		t.Fatalf("a container that is named is used as it is: %q %v", got, err)
	}
	// the shell too: the terminal opens in the default container
	sh, err := s.K8sShellSession(sid, "prod", "shop", "multi", "")
	if err != nil {
		t.Fatal(err)
	}
	tid, err := h.app.Session.Open(sh.HostID, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	h.rec.wait(t, "term:ready:"+tid)
	if exec := f.called("GET", "/api/v1/namespaces/shop/pods/multi/exec"); len(exec) != 1 || !strings.Contains(exec[0], "container=aws-ssm") {
		t.Fatalf("exec = %v", exec)
	}
}

func (k testKubeClient) Run(ctx context.Context, ns, pod, container string, command []string, stdout io.Writer) (KubeRun, error) {
	r, err := k.Client.Run(ctx, ns, pod, container, command, stdout)
	return KubeRun{ExitCode: r.ExitCode, Stderr: r.Stderr}, err
}

func TestClusterFollowsTheLogOfAPodThroughTheAPIServer(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	o := K8sLogOpts{Lines: 300, Container: "api", Timestamps: true}

	if err := s.K8sLogsFollowStart(sid, "bad key!", "prod", "shop", "api-7d9f6c-x2kq8", o); err == nil {
		t.Fatal("a key with a space was accepted")
	}
	if err := s.K8sLogsFollowStart(sid, "follow-key-1", "prod", "shop", "api; id", o); err == nil {
		t.Fatal("a pod name with a command was accepted")
	}
	if err := s.K8sLogsFollowStart(sid, "follow-key-1", "prod", "shop", "api-7d9f6c-x2kq8", K8sLogOpts{Lines: 300, Previous: true}); err == nil {
		t.Fatal("the log of an earlier run was followed")
	}
	if err := s.K8sLogsFollowStart(sid, "follow-key-1", "prod", "shop", "api-7d9f6c-x2kq8", o); err != nil {
		t.Fatal(err)
	}
	lines := h.rec.wait(t, "server:log:follow-key-1").data.(map[string]any)["lines"].([]string)
	if len(lines) != 2 || lines[0] != "line one" || lines[1] != "line two" {
		t.Fatalf("%q", lines)
	}
	h.rec.wait(t, "server:logend:follow-key-1") // the fake cluster closes the answer: the log ended
	got := f.called("GET", "/api/v1/namespaces/shop/pods/api-7d9f6c-x2kq8/log?")
	if len(got) != 1 || !strings.Contains(got[0], "follow=true") || !strings.Contains(got[0], "tailLines=300") || !strings.Contains(got[0], "container=api") || !strings.Contains(got[0], "timestamps=true") {
		t.Fatalf("log request = %v", got)
	}
}

func TestClusterKeepsTheNamespaceItOpensOn(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	h.core.Kube = testKube{}
	if _, err := h.app.Cluster.Add(ClusterInput{Label: "bad", Kubeconfig: f.kubeconfig("{token: tok}"), DefaultNamespace: "Shop; id"}); err == nil {
		t.Fatal("a namespace with a space and a command was accepted")
	}
	cl, err := h.app.Cluster.Add(ClusterInput{Label: "prod", Kubeconfig: f.kubeconfig("{token: tok}"), DefaultNamespace: " shop "})
	if err != nil || cl.DefaultNamespace != "shop" {
		t.Fatalf("%+v %v", cl, err)
	}
	got := h.app.Cluster.List()
	if len(got) != 1 || got[0].DefaultNamespace != "shop" {
		t.Fatalf("the namespace is not kept: %+v", got)
	}
	cl.DefaultNamespace = "Bad_Name"
	if _, err := h.app.Cluster.Update(cl); err == nil {
		t.Fatal("an update with a bad namespace was accepted")
	}
	cl.DefaultNamespace = ""
	if up, err := h.app.Cluster.Update(cl); err != nil || up.DefaultNamespace != "" {
		t.Fatalf("it can be emptied: %+v %v", up, err)
	}
}
