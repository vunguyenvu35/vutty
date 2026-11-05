package usecase

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"vutty/internal/domain"
)

const helmManifest = "---\n# Source: nginx/templates/secret.yaml\napiVersion: v1\nkind: Secret\nmetadata:\n  name: web-auth\n  annotations:\n    kubectl.kubernetes.io/last-applied-configuration: aHVudGVyMg==\ndata:\n  password: aHVudGVyMg==\nstringData:\n  token: plain-token\n" +
	"---\n# Source: nginx/templates/deploy.yaml\napiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: web\n  labels: {app: web}   # keeps its comment\nspec:\n  replicas: 3\n"

// helmEnc is what `kubectl get secret -o go-template={{.data.release}}` prints for a release: base64 of Helm's base64 of gzip of JSON.
func helmEnc(t *testing.T, release string) string {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write([]byte(release)); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()
	return base64.StdEncoding.EncodeToString([]byte(base64.StdEncoding.EncodeToString(b.Bytes())))
}

func helmRelease(rev int, status string) string {
	return string(jsonOf(map[string]any{
		"name": "web", "version": rev,
		"info":     map[string]any{"status": status, "description": "Upgrade complete", "notes": "Visit http://web"},
		"chart":    map[string]any{"metadata": map[string]any{"name": "nginx", "version": "15.1.0", "appVersion": "1.25.3"}},
		"config":   map[string]any{"replicas": 3, "auth": map[string]any{"password": "hunter2"}},
		"manifest": helmManifest,
	}))
}

// the labels the release Secrets of a cluster carry: web has three revisions, db one
func helmLabels(ns, name string, rev int, status string) string {
	return fmt.Sprintf("%s|%s|%d|%s|%d|2026-10-01T00:00:00Z", ns, name, rev, status, time.Now().Add(-2*time.Hour).Unix())
}

func helmRows(selector string) []string {
	if strings.Contains(selector, "name=web") {
		return []string{helmLabels("shop", "web", 1, "superseded"), helmLabels("shop", "web", 2, "superseded"), helmLabels("shop", "web", 3, "deployed")}
	}
	return []string{helmLabels("shop", "web", 3, "deployed"), helmLabels("data", "db", 1, "failed")}
}

func helmItems(selector string) string {
	var items []string
	for _, r := range helmRows(selector) {
		f := strings.Split(r, "|")
		items = append(items, fmt.Sprintf(`{"metadata":{"namespace":%q,"creationTimestamp":%q,"labels":{"owner":"helm","name":%q,"version":%q,"status":%q,"modifiedAt":%q}},"data":{"release":"not read for a list"}}`, f[0], f[5], f[1], f[2], f[3], f[4]))
	}
	return `{"items":[` + strings.Join(items, ",") + `]}`
}

func checkHelmReads(t *testing.T, h *harness, sid string, audited func() int) {
	t.Helper()
	s := h.app.Server
	list, err := s.K8sHelmReleases(sid, "prod", "")
	if err != nil || len(list) != 2 || list[0].Name != "db" || list[0].Namespace != "data" || list[0].Status != "failed" || list[1].Name != "web" || list[1].Revision != 3 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if a := list[1].AgeSec; a < 7100 || a > 7400 {
		t.Fatalf("age comes from the modifiedAt label: %d", a)
	}
	d, err := s.K8sHelmRelease(sid, "prod", "shop", "web", 0)
	if err != nil {
		t.Fatal(err)
	}
	if d.Chart != "nginx" || d.ChartVersion != "15.1.0" || d.AppVersion != "1.25.3" || d.Description != "Upgrade complete" || d.Notes != "Visit http://web" ||
		!d.HasValues || d.Release.Revision != 3 || d.Release.Status != "deployed" || d.Release.AgeSec < 7100 {
		t.Fatalf("detail = %+v", d)
	}
	if len(d.History) != 3 || d.History[0].Revision != 3 || d.History[2].Revision != 1 || d.History[2].Status != "superseded" {
		t.Fatalf("history = %+v", d.History)
	}
	for _, leak := range []string{"hunter2", "aHVudGVyMg", "plain-token"} {
		if strings.Contains(d.Manifest, leak) {
			t.Fatalf("the manifest shows %q:\n%s", leak, d.Manifest)
		}
	}
	if !strings.Contains(d.Manifest, "<hidden>") || !strings.Contains(d.Manifest, "name: web-auth") || !strings.Contains(d.Manifest, "keeps its comment") || !strings.Contains(d.Manifest, "replicas: 3") {
		t.Fatalf("the manifest is kept apart from the Secret data:\n%s", d.Manifest)
	}
	if _, err := s.K8sHelmRelease(sid, "prod", "shop", "web", 2); err != nil {
		t.Fatalf("an older revision: %v", err)
	}
	if _, err := s.K8sHelmRelease(sid, "prod", "shop", "web", 9); err == nil {
		t.Fatal("a revision that does not exist was read")
	}
	if a := audited(); a != 0 {
		t.Fatalf("reading a release is not a reveal: %d", a)
	}
	v, err := s.K8sHelmValues(sid, "prod", "shop", "web", 0)
	if err != nil || !strings.Contains(v, "password: hunter2") || !strings.Contains(v, "replicas: 3") {
		t.Fatalf("values = %q, %v", v, err)
	}
	if a := audited(); a != 1 {
		t.Fatalf("the reveal of the values was not audited once: %d", a)
	}
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.Contains(e.Message, "hunter2") {
			t.Fatalf("the audit log holds a value: %q", e.Message)
		}
	}
}

func revealedValues(h *harness, t *testing.T) func() int {
	return func() int {
		n := 0
		for _, e := range h.logs(t, domain.ActivityFilter{}) {
			if strings.Contains(e.Message, "revealed the values of Helm release web (revision 3) in shop (prod)") {
				n++
			}
		}
		return n
	}
}

func TestHelmReleasesOnAHostReadLabelsFirstAndTheReleaseWhenOpened(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "sh.helm.release.v1.web.v3"):
			return helmEnc(t, helmRelease(3, "deployed")), "", 0
		case strings.Contains(cmd, "sh.helm.release.v1.web.v2"):
			return helmEnc(t, helmRelease(2, "superseded")), "", 0
		case strings.Contains(cmd, "sh.helm.release.v1.web.v9"):
			return "", "secrets \"sh.helm.release.v1.web.v9\" not found", 1
		case strings.Contains(cmd, "get secrets -l"):
			sel := "owner=helm,status!=superseded"
			if strings.Contains(cmd, "name=web") {
				sel = "owner=helm,name=web"
			}
			return strings.Join(helmRows(sel), "\n") + "\n", "", 0
		}
		return "", "", 0
	})
	checkHelmReads(t, h, sid, revealedValues(h, t))
	if c := fake.find("owner=helm,status!=superseded"); len(c) == 0 || !strings.Contains(c[0], "kubectl get secrets -l 'owner=helm,status!=superseded' -o") || !strings.Contains(c[0], " -A --context prod") {
		t.Fatalf("the list reads labels only, in all namespaces: %q", c)
	}
	for _, c := range fake.find("get secrets -l") {
		if strings.Contains(c, ".data") {
			t.Fatalf("a list reads no Secret data: %q", c)
		}
	}
}

func TestHelmReleasesOfAClusterOpenedWithAKubeconfig(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	checkHelmReads(t, h, sid, revealedValues(h, t))
	if got := f.called("GET", "/api/v1/secrets?labelSelector=owner%3Dhelm%2Cstatus%21%3Dsuperseded"); len(got) == 0 {
		t.Fatalf("all namespaces use the cluster-wide path: %v", f.calls)
	}
}

func TestHelmRefusesBadInputBeforeAnyCommand(t *testing.T) {
	s := NewServerService(nil)
	for _, bad := range k8sBadWords {
		if _, err := s.K8sHelmRelease("x", "c", "ns", bad, 0); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("name %q: %v", bad, err)
		}
		if _, err := s.K8sHelmRelease("x", "c", bad, "web", 0); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("namespace %q: %v", bad, err)
		}
		if _, err := s.K8sHelmValues("x", bad, "ns", "web", 0); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("context %q: %v", bad, err)
		}
	}
	for _, name := range []string{"Web", "web_1", "-web", "web-", "a" + strings.Repeat("b", 60)} {
		if _, err := s.K8sHelmRelease("x", "c", "ns", name, 0); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("release %q: %v", name, err)
		}
	}
	if _, err := s.K8sHelmRelease("x", "c", "ns", "web", -1); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
		t.Errorf("revision -1: %v", err)
	}
	if _, err := helmDecode("not base64 !"); err == nil {
		t.Error("garbage was decoded")
	}
	if _, err := helmDecode("<no value>"); err == nil {
		t.Error("a missing release was decoded")
	}
}

func TestRedactManifestKeepsWhatIsNotASecretAsItWas(t *testing.T) {
	plain := "---\n# Source: a/deploy.yaml\nkind: Deployment   # a comment\nmetadata: {name: x}\n"
	if got := redactManifest(plain); got != plain {
		t.Fatalf("%q", got)
	}
	got := redactManifest("kind: ConfigMap\ndata:\n  note: mentions a Secret in a text\n---\nkind: Secret\ndata:\n  a: Zm9v\n---\nkind: Secret\n  broken: [yaml\n")
	if !strings.Contains(got, "mentions a Secret") || strings.Contains(got, "Zm9v") || !strings.Contains(got, "a: <hidden>") || !strings.Contains(got, "could not be read is left out") {
		t.Fatalf("%q", got)
	}
}
