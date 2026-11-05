package usecase

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"vutty/internal/testssh"
)

const apiDeployment = `{"apiVersion":"apps/v1","kind":"Deployment",
 "metadata":{"name":"api","namespace":"shop","resourceVersion":"41","uid":"u-1","managedFields":[{"manager":"x"}]},
 "spec":{"replicas":3,"template":{"spec":{"serviceAccountName":"api-sa",
   "initContainers":[{"name":"migrate","image":"registry/api:1.9","command":["./migrate"]}],
   "containers":[
    {"name":"api","image":"registry/api:1.9","args":["--port","8080"],"ports":[{"name":"http","containerPort":8080}],
     "env":[{"name":"LEVEL","value":"info"},{"name":"DB_PASS","valueFrom":{"secretKeyRef":{"name":"db","key":"password"}}},{"name":"POD","valueFrom":{"fieldRef":{"fieldPath":"metadata.name"}}}],
     "envFrom":[{"configMapRef":{"name":"api-config"}},{"secretRef":{"name":"api-secret"}}],
     "resources":{"requests":{"cpu":"250m","memory":"256Mi"},"limits":{"memory":"512Mi"}},
     "volumeMounts":[{"name":"data","mountPath":"/data"},{"name":"tls","mountPath":"/tls","readOnly":true}]},
    {"name":"nginx","image":"nginx:1.27"}]}}},
 "status":{"replicas":3,"readyReplicas":3}}`

// workloadCluster serves one Deployment, keeps what was sent to change it, and answers a dry run with what it was sent.
type workloadCluster struct {
	*fakeCluster
	mu       sync.Mutex
	puts     []string
	patches  []string
	conflict bool
}

func newWorkloadCluster(t *testing.T) *workloadCluster {
	t.Helper()
	w := &workloadCluster{fakeCluster: newFakeCluster(t)}
	inner := w.Server.Config.Handler
	w.Server.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/apps/v1/namespaces/shop/deployments/api" {
			inner.ServeHTTP(rw, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.mu.Lock()
		defer w.mu.Unlock()
		switch r.Method {
		case "GET":
			_, _ = rw.Write([]byte(apiDeployment))
		case "PUT":
			w.puts = append(w.puts, r.URL.RawQuery+" "+string(body))
			if w.conflict {
				rw.WriteHeader(409)
				_, _ = rw.Write([]byte(`{"kind":"Status","message":"Operation cannot be fulfilled on deployments.apps \"api\": the object has been modified; please apply your changes to the latest version and try again"}`))
				return
			}
			_, _ = rw.Write(body) // the cluster stores what it was sent
		case "PATCH":
			w.patches = append(w.patches, r.Header.Get("Content-Type")+" "+string(body))
			_, _ = rw.Write([]byte(`{}`))
		}
	})
	return w
}

func workloadSession(t *testing.T) (*harness, *workloadCluster, string) {
	h := newHarness(t).unlocked(t)
	w := newWorkloadCluster(t)
	sid, _ := clusterSession(t, h, w.fakeCluster, "{token: tok}")
	return h, w, sid
}

func auditText(h *harness) string {
	logs, _ := h.app.Log.List(domainActivityFilter())
	var text []string
	for _, e := range logs {
		text = append(text, e.Message)
	}
	return strings.Join(text, "\n")
}

func TestWorkloadSpecShowsTheContainersAndWhereValuesComeFrom(t *testing.T) {
	h, _, sid := workloadSession(t)
	sp, err := h.app.Server.K8sWorkloadSpec(sid, "prod", "deployment", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if sp.Kind != "Deployment" || sp.Replicas == nil || *sp.Replicas != 3 || sp.ServiceAccount != "api-sa" || len(sp.Containers) != 3 {
		t.Fatalf("spec = %+v", sp)
	}
	if !sp.Containers[0].Init || sp.Containers[0].Name != "migrate" || sp.Containers[0].Command[0] != "./migrate" {
		t.Fatalf("the init container comes first: %+v", sp.Containers[0])
	}
	api := sp.Containers[1]
	from := map[string]string{}
	for _, e := range api.Env {
		from[e.Name] = e.From
	}
	if api.Env[0].Value != "info" || from["DB_PASS"] != "secret db:password" || from["POD"] != "field metadata.name" || from["LEVEL"] != "" {
		t.Fatalf("env = %+v", api.Env)
	}
	if strings.Join(api.EnvFrom, "|") != "configmap api-config|secret api-secret" || api.Ports[0] != "http 8080/TCP" || api.Requests["cpu"] != "250m" || api.Limits["memory"] != "512Mi" {
		t.Fatalf("container = %+v", api)
	}
	if api.Mounts[1] != "tls → /tls (read only)" || sp.Containers[2].Env == nil || sp.Containers[2].Requests == nil {
		t.Fatalf("mounts or empty lists: %+v %+v", api.Mounts, sp.Containers[2])
	}
	if _, err := h.app.Server.K8sWorkloadSpec(sid, "prod", "pod", "shop", "api"); err == nil {
		t.Fatal("only a Deployment, StatefulSet or DaemonSet has this screen")
	}
	if _, err := h.app.Server.K8sWorkloadSpec(sid, "prod", "deployment", "shop", "a b"); err == nil {
		t.Fatal("a bad name is refused")
	}
}

func TestEditableYAMLLeavesOutWhatIsTheClustersAndKeepsTheVersion(t *testing.T) {
	h, _, sid := workloadSession(t)
	e, err := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if e.Version != "41" || strings.Contains(e.YAML, "managedFields") || strings.Contains(e.YAML, "status:") || !strings.Contains(e.YAML, "resourceVersion:") || !strings.Contains(e.YAML, "kind: Deployment") {
		t.Fatalf("editable = %+v\n%s", e, e.YAML)
	}
}

func TestEditPreviewIsADryRunAndShowsWhatWouldChange(t *testing.T) {
	h, w, sid := workloadSession(t)
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	edited := strings.Replace(e.YAML, "replicas: 3", "replicas: 5", 1)
	p, err := h.app.Server.K8sEditPreview(sid, "prod", "deployment", "shop", "api", edited)
	if err != nil {
		t.Fatal(err)
	}
	if p.Changed != 2 { // one line out, one line in
		t.Fatalf("changed = %d, diff = %+v", p.Changed, p.Diff)
	}
	var plus, minus string
	for _, l := range p.Diff {
		if l.Op == "+" {
			plus = l.Text
		}
		if l.Op == "-" {
			minus = l.Text
		}
	}
	if !strings.Contains(plus, "replicas: 5") || !strings.Contains(minus, "replicas: 3") {
		t.Fatalf("diff = %+v", p.Diff)
	}
	w.mu.Lock()
	puts := append([]string{}, w.puts...)
	w.mu.Unlock()
	if len(puts) != 1 || !strings.Contains(puts[0], "dryRun=All") || !strings.Contains(puts[0], "fieldValidation=Strict") {
		t.Fatalf("a preview is a dry run that checks the fields: %v", puts)
	}
	same, err := h.app.Server.K8sEditPreview(sid, "prod", "deployment", "shop", "api", e.YAML)
	if err != nil || same.Changed != 0 || len(same.Diff) != 0 {
		t.Fatalf("no change: %+v %v", same, err)
	}
}

func TestEditRefusesWhatIsNotTheSameObjectBeforeAnythingIsSent(t *testing.T) {
	h, w, sid := workloadSession(t)
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	cases := map[string]string{
		"another name":      strings.Replace(e.YAML, "name: api\n", "name: other\n", 1),
		"another namespace": strings.Replace(e.YAML, "namespace: shop", "namespace: kube-system", 1),
		"another kind":      strings.Replace(e.YAML, "kind: Deployment", "kind: StatefulSet", 1),
		"not yaml":          "foo: [bar",
		"empty":             "",
		"too long":          e.YAML + strings.Repeat("# x\n", maxEditYAML/4),
	}
	for name, y := range cases {
		if _, err := h.app.Server.K8sEditPreview(sid, "prod", "deployment", "shop", "api", y); err == nil {
			t.Errorf("%s: preview accepted", name)
		}
		if err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", y); err == nil {
			t.Errorf("%s: apply accepted", name)
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.puts) != 0 {
		t.Fatalf("nothing may be sent for a YAML that is refused: %v", w.puts)
	}
}

func TestEditApplyReplacesTheObjectAndIsAuditedWithoutItsText(t *testing.T) {
	h, w, sid := workloadSession(t)
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	edited := strings.Replace(e.YAML, "value: info", "value: SECRET-TOKEN-123", 1)
	if err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", edited); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	if len(w.puts) != 1 || strings.Contains(w.puts[0], "dryRun") || !strings.Contains(w.puts[0], "SECRET-TOKEN-123") || !strings.Contains(w.puts[0], `"resourceVersion":"41"`) {
		t.Fatalf("puts = %v", w.puts)
	}
	w.mu.Unlock()
	a := auditText(h)
	if !strings.Contains(a, "edited the YAML of deployment api in shop") || !strings.Contains(a, "lines changed") || strings.Contains(a, "SECRET-TOKEN-123") {
		t.Fatalf("audit:\n%s", a)
	}
}

func TestEditApplyTellsWhenSomeoneElseChangedItMeanwhile(t *testing.T) {
	h, w, sid := workloadSession(t)
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	w.conflict = true
	err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", strings.Replace(e.YAML, "replicas: 3", "replicas: 4", 1))
	if err == nil || !strings.Contains(err.Error(), "the object has been modified") {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(auditText(h), "Could not: edited the YAML") {
		t.Fatalf("a failure is audited too:\n%s", auditText(h))
	}
}

func TestSetContainerSendsOnlyWhatChangedAndNamesNoValueInTheAudit(t *testing.T) {
	h, w, sid := workloadSession(t)
	err := h.app.Server.K8sSetContainer(sid, "prod", "deployment", "shop", "api", K8sContainerEdit{
		Container: "api", Image: "registry/api:2.0",
		Env:      []K8sEnvEdit{{Name: "LEVEL", Value: "debug-VALUE-XYZ"}, {Name: "DB_PASS", Remove: true}},
		Requests: map[string]string{"cpu": "500m"}, Limits: map[string]string{"memory": ""}})
	if err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	if len(w.patches) != 1 || !strings.HasPrefix(w.patches[0], "application/strategic-merge-patch+json ") {
		t.Fatalf("patches = %v", w.patches)
	}
	var p struct {
		Spec struct {
			Template struct {
				Spec struct{ Containers []map[string]any }
			}
		}
	}
	_ = json.Unmarshal([]byte(strings.SplitN(w.patches[0], " ", 2)[1]), &p)
	w.mu.Unlock()
	c := p.Spec.Template.Spec.Containers[0]
	env, _ := c["env"].([]any)
	res, _ := c["resources"].(map[string]any)
	lim, _ := res["limits"].(map[string]any)
	if c["name"] != "api" || c["image"] != "registry/api:2.0" || len(env) != 2 || res["requests"].(map[string]any)["cpu"] != "500m" {
		t.Fatalf("patch = %v", c)
	}
	if v, ok := lim["memory"]; !ok || v != nil {
		t.Fatalf("an empty limit removes it (a null in the patch): %v", lim)
	}
	if env[1].(map[string]any)["$patch"] != "delete" {
		t.Fatalf("a removed variable is a delete: %v", env)
	}
	a := auditText(h)
	if !strings.Contains(a, "container api of deployment api in shop") || !strings.Contains(a, "DB_PASS, LEVEL") || strings.Contains(a, "debug-VALUE-XYZ") {
		t.Fatalf("audit:\n%s", a)
	}
	// an init container is edited under its own key
	if err := h.app.Server.K8sSetContainer(sid, "prod", "deployment", "shop", "api", K8sContainerEdit{Container: "migrate", Init: true, Image: "registry/api:2.0"}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !strings.Contains(w.patches[1], `"initContainers"`) {
		t.Fatalf("patch = %s", w.patches[1])
	}
}

func TestSetContainerRefusesWhatIsNotAnImageAVariableOrAQuantity(t *testing.T) {
	h, w, sid := workloadSession(t)
	bad := map[string]K8sContainerEdit{
		"no change":      {Container: "api"},
		"bad container":  {Container: "a b", Image: "x"},
		"bad image":      {Container: "api", Image: "x; rm -rf /"},
		"empty image":    {Container: "api", Image: "-x"},
		"bad env name":   {Container: "api", Env: []K8sEnvEdit{{Name: "A B", Value: "1"}}},
		"env twice":      {Container: "api", Env: []K8sEnvEdit{{Name: "A", Value: "1"}, {Name: "A", Remove: true}}},
		"env value NUL":  {Container: "api", Env: []K8sEnvEdit{{Name: "A", Value: "a\x00b"}}},
		"bad quantity":   {Container: "api", Requests: map[string]string{"cpu": "lots"}},
		"bad resource":   {Container: "api", Limits: map[string]string{"gpu": "1"}},
		"huge env value": {Container: "api", Env: []K8sEnvEdit{{Name: "A", Value: strings.Repeat("x", maxEnvValue+1)}}},
	}
	for name, e := range bad {
		if err := h.app.Server.K8sSetContainer(sid, "prod", "deployment", "shop", "api", e); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := h.app.Server.K8sSetContainer("no-such-session", "prod", "deployment", "shop", "api", K8sContainerEdit{Container: "api", Image: "x:1"}); err == nil {
		t.Fatalf("a closed session cannot make this change: %v", err)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.patches) != 0 {
		t.Fatalf("nothing may be sent: %v", w.patches)
	}
}

func TestAddSidecarAddsANewContainerAndRefusesAnExistingName(t *testing.T) {
	h, w, sid := workloadSession(t)
	s := h.app.Server
	if err := s.K8sAddSidecar(sid, "prod", "deployment", "shop", "api", K8sSidecar{Name: "logship", Image: "fluent/fluent-bit:3", Args: []string{"-c", "/etc/x.conf"}}); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	if len(w.patches) != 1 || !strings.Contains(w.patches[0], `"name":"logship"`) || !strings.Contains(w.patches[0], `"args":["-c","/etc/x.conf"]`) {
		t.Fatalf("patches = %v", w.patches)
	}
	w.mu.Unlock()
	if !strings.Contains(auditText(h), "added the sidecar logship (fluent/fluent-bit:3) to deployment api in shop") {
		t.Fatalf("audit:\n%s", auditText(h))
	}
	if err := s.K8sAddSidecar(sid, "prod", "deployment", "shop", "api", K8sSidecar{Name: "nginx", Image: "nginx:1"}); err == nil || !strings.Contains(err.Error(), "already there") {
		t.Fatalf("an existing name: %v", err)
	}
	for name, c := range map[string]K8sSidecar{"upper": {Name: "Log", Image: "x:1"}, "space": {Name: "a b", Image: "x:1"}, "no image": {Name: "ok", Image: ""},
		"bad image": {Name: "ok", Image: "x y"}, "long": {Name: strings.Repeat("a", 64), Image: "x:1"}, "NUL": {Name: "ok", Image: "x:1", Args: []string{"a\x00"}}} {
		if err := s.K8sAddSidecar(sid, "prod", "deployment", "shop", "api", c); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestSaveYAMLWritesToDownloadsPrivatelyAndNeverOverAFile(t *testing.T) {
	h, _, sid := workloadSession(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	p1, err := h.app.Server.K8sSaveYAML(sid, "prod", "deployment", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	if p1 != filepath.Join(home, "Downloads", "shop-api-deployment.yaml") {
		t.Fatalf("path = %s", p1)
	}
	st, _ := os.Stat(p1)
	b, _ := os.ReadFile(p1)
	if st.Mode().Perm() != 0o600 || !strings.Contains(string(b), "kind: Deployment") || strings.Contains(string(b), "status:") {
		t.Fatalf("file: %v\n%s", st.Mode(), b)
	}
	p2, err := h.app.Server.K8sSaveYAML(sid, "prod", "deployment", "shop", "api")
	if err != nil || p2 == p1 || !strings.HasSuffix(p2, "-2.yaml") {
		t.Fatalf("a second save must not replace the first: %s %v", p2, err)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatal("the first file is gone")
	}
	if !strings.Contains(auditText(h), "saved the YAML of deployment api") || strings.Contains(auditText(h), home) {
		t.Fatalf("audit:\n%s", auditText(h))
	}
}

func TestLineDiffKeepsContextAndCountsChanges(t *testing.T) {
	a := strings.Split("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl", "\n")
	b := append([]string{}, a...)
	b[1] = "B"
	b[10] = "K"
	d, n := lineDiff(a, b)
	if n != 4 {
		t.Fatalf("changed = %d", n)
	}
	ops := ""
	for _, l := range d {
		ops += l.Op
	}
	if !strings.Contains(ops, "~") || strings.Count(ops, "+") != 2 || strings.Count(ops, "-") != 2 {
		t.Fatalf("ops = %q: far-apart changes are separated by a gap", ops)
	}
	if d, n := lineDiff(a, a); n != 0 || len(d) != 0 {
		t.Fatalf("same text: %v %d", d, n)
	}
	long := make([]string, maxDiffLines+1)
	if d, n := lineDiff(long, a); n == 0 || d[0].Op != "~" {
		t.Fatalf("too long to compare: %v %d", d, n)
	}
}

func TestEditRightIsAskedAndHostsNeverGetThePutSide(t *testing.T) {
	h, w, sid := workloadSession(t)
	if _, err := h.app.Server.K8sAccess(sid, "prod", "shop"); err != nil {
		t.Fatal(err)
	}
	var edit int
	for _, c := range w.called("POST", "/apis/authorization.k8s.io/v1/selfsubjectaccessreviews") {
		if strings.Contains(c, `"verb":"update"`) && strings.Contains(c, `"resource":"deployments"`) {
			edit++
		}
	}
	if edit != 1 {
		t.Fatalf("the right to update a Deployment is asked once: %d", edit)
	}
	for _, kind := range []string{"pod", "secret", "service"} {
		if _, err := h.app.Server.K8sEditable(sid, "prod", kind, "shop", "x"); err == nil {
			t.Errorf("kind %s has no edit screen", kind)
		}
	}
}

// ---- the same changes on a host ----

// hostCluster is a host with kubectl: it answers a get with the Deployment, and a replace with what it was sent on its standard input.
type hostCluster struct {
	mu       sync.Mutex
	cmds     []string // every command line
	bodies   []string // what was on the standard input of each replace
	conflict bool
	oldKube  bool // refuses --validate=strict like a kubectl before 1.25
}

func (c *hostCluster) server(t *testing.T) *testssh.Server {
	t.Helper()
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw",
		ExecFunc: func(cmd string) (string, string, int) {
			c.mu.Lock()
			c.cmds = append(c.cmds, cmd)
			c.mu.Unlock()
			if strings.Contains(cmd, "get deployment api") {
				return apiDeployment, "", 0
			}
			return "", "", 0
		},
		StreamMatch: func(cmd string) bool { return strings.Contains(cmd, " replace ") },
		ExecStream: func(cmd string, stdin io.Reader, stdout, stderr io.Writer) int {
			body, _ := io.ReadAll(stdin)
			c.mu.Lock()
			defer c.mu.Unlock()
			c.cmds = append(c.cmds, cmd)
			if c.oldKube && strings.Contains(cmd, "--validate=strict") {
				_, _ = io.WriteString(stderr, `invalid argument "strict" for "--validate" flag: strconv.ParseBool: parsing "strict": invalid syntax`)
				return 1
			}
			if c.conflict {
				_, _ = io.WriteString(stderr, `Error from server (Conflict): Operation cannot be fulfilled on deployments.apps "api": the object has been modified; please apply your changes to the latest version and try again`)
				return 1
			}
			c.bodies = append(c.bodies, string(body))
			_, _ = stdout.Write(body)
			return 0
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv
}

func hostSession(t *testing.T) (*harness, *hostCluster, string) {
	h := newHarness(t).unlocked(t)
	c := &hostCluster{}
	return h, c, h.openServer(t, c.server(t))
}

func (c *hostCluster) replaces() (cmds, bodies []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, x := range c.cmds {
		if strings.Contains(x, " replace ") {
			cmds = append(cmds, x)
		}
	}
	return cmds, append([]string{}, c.bodies...)
}

func TestEditYAMLOnAHostPreviewsWithADryRunAndAppliesWithReplace(t *testing.T) {
	h, c, sid := hostSession(t)
	e, err := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(e.YAML, "replicas: 3", "replicas: 5", 1)
	p, err := h.app.Server.K8sEditPreview(sid, "prod", "deployment", "shop", "api", text)
	if err != nil || p.Changed == 0 {
		t.Fatalf("%v %+v", err, p)
	}
	if cmds, _ := c.replaces(); len(cmds) != 1 || !strings.Contains(cmds[0], "--dry-run=server") || !strings.Contains(cmds[0], "-f -") || !strings.Contains(cmds[0], "--context prod") {
		t.Fatalf("preview: %q", cmds)
	}
	if err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", text); err != nil {
		t.Fatal(err)
	}
	cmds, bodies := c.replaces()
	if len(cmds) != 2 || strings.Contains(cmds[1], "--dry-run") {
		t.Fatalf("apply: %q", cmds)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[1], `"replicas":5`) || !strings.Contains(bodies[1], `"resourceVersion":"41"`) {
		t.Fatalf("body: %q", bodies)
	}
	if !strings.Contains(auditText(h), "edited the YAML of deployment api in shop") {
		t.Fatalf("audit: %s", auditText(h))
	}
}

func TestEditYAMLOnAHostSaysWhenSomeoneChangedTheObjectFirst(t *testing.T) {
	h, c, sid := hostSession(t)
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	c.conflict = true
	err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", strings.Replace(e.YAML, "replicas: 3", "replicas: 4", 1))
	if err == nil || !strings.Contains(err.Error(), "has been modified") {
		t.Fatalf("%v", err)
	}
}

func TestEditYAMLOnAHostFallsBackForAKubectlWithoutStrictValidation(t *testing.T) {
	h, c, sid := hostSession(t)
	c.oldKube = true
	e, _ := h.app.Server.K8sEditable(sid, "prod", "deployment", "shop", "api")
	if err := h.app.Server.K8sEditApply(sid, "prod", "deployment", "shop", "api", strings.Replace(e.YAML, "replicas: 3", "replicas: 2", 1)); err != nil {
		t.Fatal(err)
	}
	cmds, bodies := c.replaces()
	if len(cmds) != 2 || !strings.Contains(cmds[1], "--validate=true") || len(bodies) != 1 {
		t.Fatalf("%q %d", cmds, len(bodies))
	}
}

func TestEditConfigOnAHostChangesTheContainerAndKeepsValuesOffTheCommandLine(t *testing.T) {
	h, c, sid := hostSession(t)
	err := h.app.Server.K8sSetContainer(sid, "prod", "deployment", "shop", "api", K8sContainerEdit{
		Container: "api", Image: "registry/api:2.0",
		Env:      []K8sEnvEdit{{Name: "LEVEL", Value: "s3cr3t-value"}, {Name: "NEW", Value: "1"}, {Name: "POD", Remove: true}},
		Requests: map[string]string{"cpu": "500m"}, Limits: map[string]string{"memory": ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmds, bodies := c.replaces()
	if len(cmds) != 2 || len(bodies) != 2 {
		t.Fatalf("%q %d", cmds, len(bodies))
	}
	c.mu.Lock()
	for _, x := range c.cmds {
		if strings.Contains(x, "s3cr3t-value") {
			t.Fatalf("a value is in a command line: %s", x)
		}
	}
	c.mu.Unlock()
	var obj map[string]any
	if err := json.Unmarshal([]byte(bodies[1]), &obj); err != nil {
		t.Fatal(err)
	}
	ps, _ := podSpecOf(obj)
	api := ps["containers"].([]any)[0].(map[string]any)
	if api["image"] != "registry/api:2.0" {
		t.Fatalf("image %v", api["image"])
	}
	env := mustJSON(api["env"])
	if !strings.Contains(string(env), `"s3cr3t-value"`) || !strings.Contains(string(env), `"NEW"`) || strings.Contains(string(env), `"POD"`) || !strings.Contains(string(env), "secretKeyRef") {
		t.Fatalf("env %s", env)
	}
	res := string(mustJSON(api["resources"]))
	if !strings.Contains(res, `"cpu":"500m"`) || strings.Contains(res, `"limits"`) {
		t.Fatalf("resources %s", res)
	}
	if strings.Contains(bodies[1], "managedFields") || strings.Contains(bodies[1], `"status"`) || !strings.Contains(bodies[1], `"resourceVersion":"41"`) {
		t.Fatalf("body %s", bodies[1])
	}
	if a := auditText(h); !strings.Contains(a, "container api") || strings.Contains(a, "s3cr3t-value") {
		t.Fatalf("audit %s", a)
	}
}

func TestEditConfigOnAHostRefusesWhatTheClusterWouldAndSendsNothing(t *testing.T) {
	h, c, sid := hostSession(t)
	for name, e := range map[string]K8sContainerEdit{
		"no such container":      {Container: "ghost", Image: "x:1"},
		"a variable from secret": {Container: "api", Env: []K8sEnvEdit{{Name: "DB_PASS", Value: "x"}}},
	} {
		if err := h.app.Server.K8sSetContainer(sid, "prod", "deployment", "shop", "api", e); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if cmds, _ := c.replaces(); len(cmds) != 0 {
		t.Fatalf("nothing may be sent: %q", cmds)
	}
}

func TestAddSidecarOnAHostAppendsTheContainerAndRefusesAnExistingName(t *testing.T) {
	h, c, sid := hostSession(t)
	if err := h.app.Server.K8sAddSidecar(sid, "prod", "deployment", "shop", "api", K8sSidecar{Name: "nginx", Image: "nginx:1"}); err == nil {
		t.Fatal("an existing name was accepted")
	}
	if err := h.app.Server.K8sAddSidecar(sid, "prod", "deployment", "shop", "api", K8sSidecar{Name: "logs", Image: "busybox:1", Command: []string{"tail"}, Args: []string{"-f", "/x"}}); err != nil {
		t.Fatal(err)
	}
	_, bodies := c.replaces()
	if len(bodies) != 2 || !strings.Contains(bodies[1], `"name":"logs"`) || !strings.Contains(bodies[1], `"name":"nginx"`) || !strings.Contains(bodies[1], `"command":["tail"]`) {
		t.Fatalf("%q", bodies)
	}
}

// The change made here on an object must be the one the cluster makes for the patch of a kubeconfig.
func TestApplyContainerEditMatchesTheMergePatch(t *testing.T) {
	var base map[string]any
	_ = json.Unmarshal([]byte(apiDeployment), &base)
	cases := []K8sContainerEdit{
		{Container: "api", Image: "x:2"},
		{Container: "api", Env: []K8sEnvEdit{{Name: "LEVEL", Value: "debug"}}},
		{Container: "api", Env: []K8sEnvEdit{{Name: "LEVEL", Remove: true}, {Name: "A", Value: "1"}}},
		{Container: "api", Requests: map[string]string{"cpu": ""}, Limits: map[string]string{"cpu": "1"}},
		{Container: "migrate", Init: true, Image: "y:3"},
	}
	for i, e := range cases {
		var obj map[string]any
		_ = json.Unmarshal([]byte(apiDeployment), &obj)
		if err := applyContainerEdit(obj, e); err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		ps, _ := podSpecOf(obj)
		key := "containers"
		if e.Init {
			key = "initContainers"
		}
		got := string(mustJSON(ps[key].([]any)[0]))
		for _, must := range wantAfter[i] {
			if !strings.Contains(got, must) {
				t.Errorf("%d: %s lacks %s", i, got, must)
			}
		}
	}
}

var wantAfter = [][]string{
	{`"image":"x:2"`},
	{`"name":"LEVEL","value":"debug"`, `secretKeyRef`},
	{`"name":"A","value":"1"`, `secretKeyRef`},
	{`"limits":{"cpu":"1","memory":"512Mi"}`, `"requests":{"memory":"256Mi"}`},
	{`"image":"y:3"`},
}
