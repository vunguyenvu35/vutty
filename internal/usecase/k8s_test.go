package usecase

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vutty/internal/domain"
	"vutty/internal/testssh"
)

// ---- what the screen sends is refused before any command ----

var k8sBadWords = []string{"x;reboot", "a b", "$(id)", "a`id`", "a\nb", "a..b", "-rf", "a'b", `a"b`, "a|b", "a&b", "a>b", "a\\b", strings.Repeat("a", 254)}

// Every method that takes a name or a number, with a bad value in each place in turn. There is no session here, so a call that
// got past its check would fail with "the connection to the server is closed" instead of a refusal.
func TestK8sRefusesBadInputBeforeAnyCommand(t *testing.T) {
	s := NewServerService(nil)
	type call struct {
		what string
		f    func(bad string) error
	}
	e := func(err error) error { return err }
	calls := []call{
		{"check ctx", func(b string) error { _, err := s.K8sCheck("x", b); return err }},
		{"access ctx", func(b string) error { _, err := s.K8sAccess("x", b, "ns"); return err }},
		{"access ns", func(b string) error { _, err := s.K8sAccess("x", "c", b); return err }},
		{"namespaces ctx", func(b string) error { _, err := s.K8sNamespaces("x", b); return err }},
		{"overview ctx", func(b string) error { _, err := s.K8sOverview("x", b); return err }},
		{"nodes ctx", func(b string) error { _, err := s.K8sNodes("x", b); return err }},
		{"pods ctx", func(b string) error { _, err := s.K8sPods("x", b, "ns"); return err }},
		{"pods ns", func(b string) error { _, err := s.K8sPods("x", "c", b); return err }},
		{"pod ns", func(b string) error { _, err := s.K8sPod("x", "c", b, "p"); return err }},
		{"pod name", func(b string) error { _, err := s.K8sPod("x", "c", "ns", b); return err }},
		{"workloads ns", func(b string) error { _, err := s.K8sWorkloads("x", "c", b); return err }},
		{"deployment name", func(b string) error { _, err := s.K8sDeployment("x", "c", "ns", b); return err }},
		{"network ns", func(b string) error { _, err := s.K8sNetwork("x", "c", b); return err }},
		{"config ns", func(b string) error { _, err := s.K8sConfig("x", "c", b); return err }},
		{"configmap name", func(b string) error { _, err := s.K8sConfigMap("x", "c", "ns", b); return err }},
		{"events ns", func(b string) error { _, err := s.K8sEvents("x", "c", b); return err }},
		{"logs pod", func(b string) error {
			_, err := s.K8sLogs("x", "c", "ns", b, K8sLogOpts{Lines: 100})
			return err
		}},
		{"logs container", func(b string) error {
			_, err := s.K8sLogs("x", "c", "ns", "p", K8sLogOpts{Lines: 100, Container: b})
			return err
		}},
		{"yaml name", func(b string) error { _, err := s.K8sYAML("x", "c", "pod", "ns", b); return err }},
		{"yaml secret name", func(b string) error { _, err := s.K8sYAML("x", "c", "secret", "ns", b); return err }},
		{"yaml ns", func(b string) error { _, err := s.K8sYAML("x", "c", "pod", b, "p"); return err }},
		{"drain plan node", func(b string) error { _, err := s.K8sDrainPlan("x", "c", b, K8sDrainOpts{}); return err }},
		{"scale name", func(b string) error { return s.K8sScale("x", "c", "ns", b, 1) }},
		{"scale ns", func(b string) error { return s.K8sScale("x", "c", b, "d", 1) }},
		{"restart name", func(b string) error { return s.K8sRestart("x", "c", "ns", "deployment", b) }},
		{"pause name", func(b string) error { return s.K8sRolloutPause("x", "c", "ns", b, true) }},
		{"rollback name", func(b string) error { return s.K8sRollback("x", "c", "ns", b, 2) }},
		{"delete pod", func(b string) error { return s.K8sDeletePod("x", "c", "ns", b) }},
		{"delete pod ns", func(b string) error { return s.K8sDeletePod("x", "c", b, "p") }},
		{"delete job", func(b string) error { return s.K8sDeleteJob("x", "c", "ns", b) }},
		{"delete job ctx", func(b string) error { return s.K8sDeleteJob("x", b, "ns", "j") }},
		{"cronjob name", func(b string) error { return s.K8sCronJob("x", "c", "ns", b, "run") }},
		{"cordon node", func(b string) error { return s.K8sCordon("x", "c", b, true) }},
		{"drain node", func(b string) error { return s.K8sDrain("x", "c", b, K8sDrainOpts{}) }},
		{"shell pod", func(b string) error { _, err := s.K8sShellCommand("x", "c", "ns", b, ""); return err }},
		{"shell container", func(b string) error { _, err := s.K8sShellCommand("x", "c", "ns", "p", b); return err }},
		{"forward service", func(b string) error { _, err := s.K8sPortForwardStart("x", "c", "ns", b, 8080, 80); return err }},
		{"forward ns", func(b string) error { _, err := s.K8sPortForwardStart("x", "c", b, "svc", 8080, 80); return err }},
		{"secret name", func(b string) error { _, err := s.K8sSecretKey("x", "c", "ns", b, "k"); return err }},
		{"secret key", func(b string) error { _, err := s.K8sSecretKey("x", "c", "ns", "n", b); return err }},
	}
	bads := append([]string{""}, k8sBadWords...)
	for _, c := range calls {
		for _, bad := range bads {
			if bad == "" && strings.Contains(c.what, "ns") && (strings.HasPrefix(c.what, "pods") || strings.HasPrefix(c.what, "workloads") ||
				strings.HasPrefix(c.what, "network") || strings.HasPrefix(c.what, "config ns") || strings.HasPrefix(c.what, "events") ||
				strings.HasPrefix(c.what, "access")) {
				continue // an empty namespace means all of them in a list
			}
			if bad == "" && strings.HasSuffix(c.what, "container") {
				continue // a pod's default container: no -c
			}
			if err := c.f(bad); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
				t.Errorf("%s: %q was not refused before a command: %v", c.what, bad, err)
			}
		}
	}
	// a context may hold / and : (an EKS ARN) but not what a shell reads
	if err := e(k8sCtx("arn:aws:eks:us-east-1:123456789012:cluster/prod")); err != nil {
		t.Errorf("an ARN context was refused: %v", err)
	}
	// numbers and words
	refused := map[string]error{
		"replicas low":   s.K8sScale("x", "c", "ns", "d", -1),
		"replicas high":  s.K8sScale("x", "c", "ns", "d", 1001),
		"revision zero":  s.K8sRollback("x", "c", "ns", "d", 0),
		"revision neg":   s.K8sRollback("x", "c", "ns", "d", -3),
		"restart kind":   s.K8sRestart("x", "c", "ns", "pod", "d"),
		"restart kind 2": s.K8sRestart("x", "c", "ns", "deployment; id", "d"),
		"cronjob action": s.K8sCronJob("x", "c", "ns", "j", "delete"),
		"cronjob upper":  s.K8sCronJob("x", "c", "ns", "My_Job", "run"),
		"drain grace":    s.K8sDrain("x", "c", "n", K8sDrainOpts{GraceSec: -1}),
		"drain grace 2":  s.K8sDrain("x", "c", "n", K8sDrainOpts{GraceSec: 99999}),
		"drain timeout":  s.K8sDrain("x", "c", "n", K8sDrainOpts{TimeoutSec: 99999}),
		"drain timeout2": s.K8sDrain("x", "c", "n", K8sDrainOpts{TimeoutSec: -5}),
		"plan grace":     func() error { _, err := s.K8sDrainPlan("x", "c", "n", K8sDrainOpts{GraceSec: -1}); return err }(),
		"logs lines":     func() error { _, err := s.K8sLogs("x", "c", "ns", "p", K8sLogOpts{Lines: 7}); return err }(),
		"logs lines 0":   func() error { _, err := s.K8sLogs("x", "c", "ns", "p", K8sLogOpts{}); return err }(),
		"logs since": func() error {
			_, err := s.K8sLogs("x", "c", "ns", "p", K8sLogOpts{Lines: 100, Since: "1h; id"})
			return err
		}(),
		"logs since 2": func() error {
			_, err := s.K8sLogs("x", "c", "ns", "p", K8sLogOpts{Lines: 100, Since: "2h"})
			return err
		}(),
		"yaml kind":      func() error { _, err := s.K8sYAML("x", "c", "pod;id", "ns", "p"); return err }(),
		"yaml kind 2":    func() error { _, err := s.K8sYAML("x", "c", "clusterrole", "ns", "p"); return err }(),
		"forward local":  func() error { _, err := s.K8sPortForwardStart("x", "c", "ns", "s", 80, 80); return err }(),
		"forward local2": func() error { _, err := s.K8sPortForwardStart("x", "c", "ns", "s", 70000, 80); return err }(),
		"forward remote": func() error { _, err := s.K8sPortForwardStart("x", "c", "ns", "s", 8080, 0); return err }(),
		"forward rem 2":  func() error { _, err := s.K8sPortForwardStart("x", "c", "ns", "s", 8080, 65536); return err }(),
		"secret key dot": func() error { _, err := s.K8sSecretKey("x", "c", "ns", "n", ".."); return err }(),
		"secret key tpl": func() error { _, err := s.K8sSecretKey("x", "c", "ns", "n", `a"}}{{.data`); return err }(),
	}
	for name, err := range refused {
		if err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%s was not refused before a command: %v", name, err)
		}
	}
	// and what is fine gets as far as the session
	ok := map[string]error{
		"scale":      s.K8sScale("x", "c", "ns", "d", 0),
		"restart":    s.K8sRestart("x", "c", "ns", "statefulset", "d"),
		"cronjob":    s.K8sCronJob("x", "c", "ns", "nightly-report", "run"),
		"secret key": func() error { _, err := s.K8sSecretKey("x", "c", "ns", "n", ".dockerconfigjson"); return err }(),
		"forward":    func() error { _, err := s.K8sPortForwardStart("x", "c", "ns", "s", 8080, 80); return err }(),
		"drain":      s.K8sDrain("x", "c", "n", K8sDrainOpts{GraceSec: 30, TimeoutSec: 300}),
		"pods all":   func() error { _, err := s.K8sPods("x", "arn:aws:eks:eu-1:1:cluster/p", ""); return err }(),
	}
	for name, err := range ok {
		if err == nil || !strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestK8sQuotingAndWords(t *testing.T) {
	for in, want := range map[string]string{"api": "api", "a=b,c": "a=b,c", "{.x}": "'{.x}'", "it's": `'it'\''s'`, "a b": "'a b'", "": "''", "$(id)": "'$(id)'"} {
		if got := shq(in); got != want {
			t.Errorf("%q: %s want %s", in, got, want)
		}
	}
}

// ---- a host that answers like kubectl ----

type k8sFake struct {
	mu   sync.Mutex
	cmds []string
	f    func(cmd string) (string, string, int)
}

func (k *k8sFake) answer(cmd string) (string, string, int) {
	k.mu.Lock()
	k.cmds = append(k.cmds, cmd)
	k.mu.Unlock()
	return k.f(cmd)
}

func (k *k8sFake) last() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.cmds[len(k.cmds)-1]
}

func (k *k8sFake) count() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return len(k.cmds)
}

func (k *k8sFake) find(sub string) []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	var out []string
	for _, c := range k.cmds {
		if strings.Contains(c, sub) {
			out = append(out, c)
		}
	}
	return out
}

func (h *harness) k8sSession(t *testing.T, f func(cmd string) (string, string, int)) (string, *k8sFake) {
	t.Helper()
	fake := &k8sFake{f: f}
	return h.openServer(t, execServer(t, fake.answer)), fake
}

var k8sSessions atomic.Int32

// openServer opens a Server session on a test server; its key and label are its own, so a test can open several.
func (h *harness) openServer(t *testing.T, srv *testssh.Server) string {
	t.Helper()
	n := k8sSessions.Add(1)
	key, label := fmt.Sprintf("k8s-key-%d", n), fmt.Sprintf("k8s-%d", n)
	host := h.sshHost(t, label, srv, "")
	type res struct {
		info ServerInfo
		err  error
	}
	done := make(chan res, 1)
	go func() { i, err := h.app.Server.Open(host.ID, key); done <- res{i, err} }()
	h.trust(t, key)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = h.app.Server.Close(r.info.SID) })
	return r.info.SID
}

func TestK8sReadsAskKubectlForJSONWithContextAndNamespace(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get events"):
			return eventsJSON, "", 0
		case strings.Contains(cmd, "get pods"):
			return podsJSON, "", 0
		case strings.Contains(cmd, "get pod api-7d9f6c-x2kq8"):
			return podsJSON[len(`{"items":[`):strings.Index(podsJSON, ",\n {\"metadata\":{\"name\":\"web-1\"")], "", 0
		case strings.Contains(cmd, "get namespaces"):
			return "namespace/shop\nnamespace/default\nnamespace/bad;name\n", "", 0
		case strings.Contains(cmd, "get deployments,statefulsets"):
			return workJSON, "", 0
		}
		return "", "", 0
	})
	pods, err := h.app.Server.K8sPods(sid, "prod", "shop")
	if err != nil || len(pods) != 8 {
		t.Fatalf("%v %d", err, len(pods))
	}
	if got := fake.find("get pods"); len(got) != 1 || !strings.HasSuffix(got[0], "kubectl get pods -o json -n shop --context prod --request-timeout=15s") {
		t.Fatalf("%q", got)
	}
	if got := fake.find("get events"); len(got) != 1 || !strings.Contains(got[0], "kubectl get events --field-selector type=Warning -o json -n shop --context prod") {
		t.Fatalf("%q", got)
	}
	// all namespaces: -A and no -n
	if _, err := h.app.Server.K8sPods(sid, "prod", ""); err != nil {
		t.Fatal(err)
	}
	if got := fake.find("get pods -o json -A"); len(got) != 1 || strings.Contains(got[0], " -n ") || !strings.Contains(got[0], "--context prod") {
		t.Fatalf("%q", got)
	}
	ns, err := h.app.Server.K8sNamespaces(sid, "prod")
	if err != nil || strings.Join(ns.Names, ",") != "default,shop" {
		t.Fatalf("%v %v", ns, err)
	}
	w, err := h.app.Server.K8sWorkloads(sid, "prod", "")
	if err != nil || len(w.Items) != 9 { // every Job is listed, the one a CronJob made too
		t.Fatalf("%v %v", w, err)
	}
	// a name with a space or a quote that the cluster itself returned never reaches a command
	d, err := h.app.Server.K8sPod(sid, "prod", "shop", "api-7d9f6c-x2kq8")
	if err != nil || d.Pod.Name != "api-7d9f6c-x2kq8" {
		t.Fatalf("%+v %v", d, err)
	}
	if got := fake.find("involvedObject.name=api-7d9f6c-x2kq8"); len(got) != 1 {
		t.Fatalf("%q", got)
	}
}

func TestK8sReadErrorsComeBackAsTheKubectlText(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "", `Error from server (Forbidden): pods is forbidden: User "u" cannot list resource "pods"` + "\n", 1
	})
	_, err := h.app.Server.K8sPods(sid, "prod", "shop")
	if err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("%v", err)
	}
}

func TestK8sLogsCommand(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) { return "line one\nline two\n", "", 0 })
	out, err := h.app.Server.K8sLogs(sid, "prod", "shop", "api-1", K8sLogOpts{Container: "api", Lines: 300, Since: "1h", Previous: true, Timestamps: true})
	if err != nil || out != "line one\nline two\n" {
		t.Fatalf("%q %v", out, err)
	}
	if want := "kubectl logs api-1 --tail=300 --since=1h -c api --previous --timestamps -n shop --context prod --request-timeout=15s"; !strings.HasSuffix(fake.last(), want) {
		t.Fatalf("%q", fake.last())
	}
}

func TestK8sAccessAsksWhatTheLoginMayDo(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		out := ""
		for _, r := range k8sRights {
			ans := "yes"
			if r[0] == "secrets" || r[0] == "drain" {
				ans = "no - RBAC: nope"
			}
			out += "@@@" + r[0] + "\n" + ans + "\n"
		}
		return out, "", 0
	})
	a, err := h.app.Server.K8sAccess(sid, "prod", "shop")
	if err != nil || len(a.Rights) != 14 || !a.Rights["list"] || a.Rights["secrets"] || a.Rights["drain"] || !a.Rights["portforward"] {
		t.Fatalf("%+v %v", a, err)
	}
	c := fake.last()
	for _, want := range []string{"echo '@@@list'; kubectl auth can-i list pods --request-timeout=10s -n shop --context prod 2>&1",
		"kubectl auth can-i create pods/exec", "kubectl auth can-i patch deployments/scale", "kubectl auth can-i update deployments", "kubectl auth can-i create pods/eviction", "kubectl auth can-i get secrets"} {
		if !strings.Contains(c, want) {
			t.Errorf("missing %q in %q", want, c)
		}
	}
	// nothing answered: that is an error the screen can show, not a screen of locked buttons
	sid2, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "@@@list\nUnable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout\n", "", 0
	})
	if _, err := h.app.Server.K8sAccess(sid2, "prod", ""); err == nil || !strings.Contains(err.Error(), "i/o timeout") {
		t.Fatalf("%v", err)
	}
}

func TestK8sCheckClassifiesWhyAClusterDoesNotAnswer(t *testing.T) {
	h := newHarness(t).unlocked(t)
	for msg, want := range map[string]string{
		"Unable to connect to the server: dial tcp 10.0.0.1:6443: i/o timeout":                                 "unreachable",
		"error: You must be logged in to the server (Unauthorized)":                                            "unauthorized",
		"Unable to connect to the server: x509: certificate signed by unknown authority":                       "certificate",
		`Error from server (Forbidden): forbidden: User "u" cannot get path "/version"`:                        "forbidden",
		"Unable to connect to the server: getting credentials: exec: executable aws failed with exit code 255": "unauthorized",
	} {
		sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
			if strings.Contains(cmd, "config view") {
				return "https://10.0.0.1:6443", "", 0
			}
			return `{"clientVersion":{}}`, msg + "\n", 1
		})
		c, err := h.app.Server.K8sCheck(sid, "prod")
		if err != nil || c.OK || c.Kind != want || !strings.Contains(c.Raw, strings.SplitN(msg, ":", 2)[0]) || c.Server != "https://10.0.0.1:6443" {
			t.Errorf("%q: %+v %v", msg, c, err)
		}
		if got := fake.find("kubectl version -o json --request-timeout=8s --context prod"); len(got) != 1 {
			t.Errorf("%q", fake.cmds)
		}
	}
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		if strings.Contains(cmd, "version") {
			return `{"clientVersion":{"gitVersion":"v1.29.2"},"serverVersion":{"gitVersion":"v1.29.4"}}`, "", 0
		}
		return "https://x", "", 0
	})
	if c, err := h.app.Server.K8sCheck(sid, "prod"); err != nil || !c.OK || c.ServerVersion != "v1.29.4" || c.Kind != "" {
		t.Fatalf("%+v %v", c, err)
	}
}

// ---- changes: the exact command, and the audit entry ----

func TestK8sChangesSendFixedCommandsAndAreAudited(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) { return "", "", 0 })
	srv := h.app.Server
	clock := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	k8sNow = func() time.Time { return clock }
	t.Cleanup(func() { k8sNow = time.Now })
	cases := []struct {
		what string
		do   func() error
		want string
	}{
		{"scale", func() error { return srv.K8sScale(sid, "prod", "shop", "api", 5) }, "kubectl scale deployment/api --replicas=5 -n shop --context prod"},
		{"restart", func() error { return srv.K8sRestart(sid, "prod", "shop", "deployment", "api") },
			"kubectl rollout restart deployment/api -n shop --context prod && kubectl rollout status deployment/api --timeout=120s -n shop --context prod"},
		{"restart sts", func() error { return srv.K8sRestart(sid, "prod", "data", "statefulset", "db") }, "kubectl rollout restart statefulset/db -n data --context prod"},
		{"pause", func() error { return srv.K8sRolloutPause(sid, "prod", "shop", "api", true) }, "kubectl rollout pause deployment/api -n shop --context prod"},
		{"resume", func() error { return srv.K8sRolloutPause(sid, "prod", "shop", "api", false) }, "kubectl rollout resume deployment/api -n shop --context prod"},
		{"rollback", func() error { return srv.K8sRollback(sid, "prod", "shop", "api", 6) }, "kubectl rollout undo deployment/api --to-revision=6 -n shop --context prod"},
		{"delete pod", func() error { return srv.K8sDeletePod(sid, "prod", "shop", "api-1") }, "kubectl delete pod api-1 --wait=false -n shop --context prod"},
		{"delete job", func() error { return srv.K8sDeleteJob(sid, "prod", "shop", "migrate-1103") }, "kubectl delete job migrate-1103 --wait=false -n shop --context prod"},
		{"cron run", func() error { return srv.K8sCronJob(sid, "prod", "shop", "nightly-report", "run") },
			"kubectl create job --from=cronjob/nightly-report nightly-report-manual-" + strconv.FormatInt(clock.Unix(), 36) + " -n shop --context prod"},
		{"cron suspend", func() error { return srv.K8sCronJob(sid, "prod", "shop", "nightly-report", "suspend") },
			`kubectl patch cronjob nightly-report -p '{"spec":{"suspend":true}}' -n shop --context prod`},
		{"cron resume", func() error { return srv.K8sCronJob(sid, "prod", "shop", "nightly-report", "resume") },
			`kubectl patch cronjob nightly-report -p '{"spec":{"suspend":false}}' -n shop --context prod`},
		{"cordon", func() error { return srv.K8sCordon(sid, "prod", "worker-1", true) }, "kubectl cordon worker-1 --context prod"},
		{"uncordon", func() error { return srv.K8sCordon(sid, "prod", "worker-1", false) }, "kubectl uncordon worker-1 --context prod"},
		{"drain", func() error {
			return srv.K8sDrain(sid, "prod", "worker-1", K8sDrainOpts{IgnoreDaemonsets: true, DeleteEmptyDir: true, GraceSec: 30, TimeoutSec: 300})
		}, "kubectl cordon worker-1 --context prod && kubectl drain worker-1 --ignore-daemonsets --delete-emptydir-data --grace-period=30 --timeout=300s --context prod"},
		{"drain plain", func() error { return srv.K8sDrain(sid, "prod", "worker-1", K8sDrainOpts{Force: true}) },
			"kubectl drain worker-1 --force --timeout=300s --context prod"},
	}
	for _, c := range cases {
		if err := c.do(); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if !strings.HasSuffix(fake.last(), c.want) && !strings.Contains(fake.last(), c.want) {
			t.Errorf("%s: %q, want %q", c.what, fake.last(), c.want)
		}
		if strings.Contains(fake.last(), "--request-timeout") {
			t.Errorf("%s: a change has no request timeout: %q", c.what, fake.last())
		}
	}
	logs := h.logs(t, domain.ActivityFilter{})
	var msgs []string
	for _, e := range logs {
		msgs = append(msgs, e.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"scaled deployment api to 5 in shop (prod)", "restarted deployment api in shop (prod)", "paused the rollout of deployment api",
		"rolled deployment api back to revision 6", "deleted pod api-1", "deleted job migrate-1103", "ran cronjob nightly-report now", "suspended cronjob nightly-report",
		"resumed cronjob nightly-report", "cordoned node worker-1", "uncordoned node worker-1", "drained node worker-1"} {
		if !strings.Contains(all, want) {
			t.Errorf("not audited: %q in\n%s", want, all)
		}
	}
}

func TestK8sFailedChangeReturnsKubectlTextAndIsAuditedAsFailed(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		if strings.Contains(cmd, "rollout status") {
			return "", "error: timed out waiting for the condition\n", 1
		}
		return "", `Error from server (Forbidden): deployments.apps "api" is forbidden: cannot patch resource "deployments/scale"` + "\n", 1
	})
	err := h.app.Server.K8sScale(sid, "prod", "shop", "api", 3)
	if err == nil || !strings.Contains(err.Error(), "cannot patch resource") {
		t.Fatalf("%v", err)
	}
	err = h.app.Server.K8sRestart(sid, "prod", "shop", "deployment", "api")
	if err == nil || !strings.HasPrefix(err.Error(), "still rolling out") {
		t.Fatalf("%v", err)
	}
	var failed int
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.HasPrefix(e.Message, "Could not: scaled deployment api") && !e.OK {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("the failure was not audited as failed")
	}
}

func TestK8sShellCommandUsesTheRememberedWayIn(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) { return "", "", 0 })
	got, err := h.app.Server.K8sShellCommand(sid, "prod", "shop", "api-1", "api")
	want := `kubectl exec -it -n shop api-1 -c api --context prod -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'`
	if err != nil || got != want {
		t.Fatalf("%q %v", got, err)
	}
	x, _ := h.app.Server.get(sid)
	x.k8s.mu.Lock()
	x.k8s.prefix, x.k8s.kubeconfig, x.k8s.sudo, x.k8s.notRoot = "k3s kubectl", "k3s", true, true
	x.k8s.mu.Unlock()
	got, _ = h.app.Server.K8sShellCommand(sid, "default", "kube-system", "coredns-1", "")
	want = `sudo k3s kubectl exec -it -n kube-system coredns-1 --context default --kubeconfig=/etc/rancher/k3s/k3s.yaml -- sh -c 'command -v bash >/dev/null && exec bash || exec sh'`
	if got != want {
		t.Fatalf("%q", got)
	}
	x.k8s.mu.Lock()
	x.k8s.notRoot = false
	x.k8s.mu.Unlock()
	if got, _ = h.app.Server.K8sShellCommand(sid, "default", "kube-system", "coredns-1", ""); strings.HasPrefix(got, "sudo") {
		t.Fatalf("root needs no sudo: %q", got)
	}
}

// ---- secrets ----

func TestK8sSecretsNeverLoadAValueExceptTheOneAskedFor(t *testing.T) {
	const value = "s3cr3t-p4ssw0rd"
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get secrets"):
			return "shop|api-tls|2026-11-02T12:00:00Z|tls.crt,tls.key,||kubernetes.io/tls\n", "", 0
		case strings.Contains(cmd, "get configmaps"):
			return "", "", 0
		case strings.Contains(cmd, "get pvc"), strings.Contains(cmd, "get pods"):
			return `{"items":[]}`, "", 0
		case strings.Contains(cmd, "get secret db"):
			if strings.Contains(cmd, `"password"`) {
				return base64.StdEncoding.EncodeToString([]byte(value)) + "\n", "", 0
			}
			if strings.Contains(cmd, "jsonpath") && strings.Contains(cmd, "@@@keys") {
				return `{"name":"db","namespace":"shop","annotations":{"kubectl.kubernetes.io/last-applied-configuration":"{\"data\":{\"password\":\"` + value + `\"}}","owner":"me"},"managedFields":[{"x":1}]}` + "\nOpaque\n@@@keys\npassword\nuser\n", "", 0
			}
			return "password\nuser\n", "", 0
		}
		return "", "", 0
	})
	cfg, err := h.app.Server.K8sConfig(sid, "prod", "shop")
	if err != nil || len(cfg.Secrets) != 1 || strings.Join(cfg.Secrets[0].Keys, ",") != "tls.crt,tls.key" {
		t.Fatalf("%+v %v", cfg, err)
	}
	for _, c := range fake.find("get secrets") {
		if strings.Contains(c, "-o json") || strings.Contains(c, "-o yaml") || strings.Contains(c, "{{$v}}") || strings.Contains(c, "{{.data}}") || strings.Contains(c, "base64decode") {
			t.Errorf("the secret list could print a value: %q", c)
		}
		if !strings.Contains(c, "range $k,$v := .data}}{{$k}}") {
			t.Errorf("%q", c)
		}
	}
	y, err := h.app.Server.K8sYAML(sid, "prod", "secret", "shop", "db")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(y, value) || strings.Contains(y, "managedFields") || strings.Contains(y, "last-applied") || !strings.Contains(y, "password: redacted") ||
		!strings.Contains(y, "user: redacted") || !strings.Contains(y, "kind: Secret") || !strings.Contains(y, "type: Opaque") || !strings.Contains(y, "owner: me") {
		t.Fatalf("%s", y)
	}
	for _, c := range fake.find("get secret db") {
		if strings.Contains(c, "-o json") || strings.Contains(c, "-o yaml") {
			t.Errorf("the whole secret was asked for: %q", c)
		}
	}
	got, err := h.app.Server.K8sSecretKey(sid, "prod", "shop", "db", "password")
	if err != nil || got != value {
		t.Fatalf("%q %v", got, err)
	}
	if c := fake.last(); !strings.Contains(c, `kubectl get secret db -o 'go-template={{index .data "password"}}' -n shop --context prod`) {
		t.Fatalf("%q", c)
	}
	if _, err := h.app.Server.K8sSecretKey(sid, "prod", "shop", "db", "missing-key"); err != nil {
		t.Logf("a key the fake does not know: %v", err)
	}
	var revealed int
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		if strings.Contains(e.Message, value) || strings.Contains(e.Message, base64.StdEncoding.EncodeToString([]byte(value))) {
			t.Fatalf("the value is in the audit log: %q", e.Message)
		}
		if strings.Contains(e.Message, "revealed key password of secret db in shop (prod)") {
			revealed++
		}
	}
	if revealed != 1 {
		t.Fatalf("the reveal was not audited once: %d", revealed)
	}
}

func TestK8sYAMLDropsManagedFields(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "apiVersion: v1\nkind: Pod\nmetadata:\n  managedFields:\n  - apiVersion: v1\n    fieldsType: FieldsV1\n    manager: kubectl\n  name: api-1\n  namespace: shop\nspec:\n  nodeName: w1\n", "", 0
	})
	y, err := h.app.Server.K8sYAML(sid, "prod", "pod", "shop", "api-1")
	if err != nil || strings.Contains(y, "managedFields") || strings.Contains(y, "manager") || !strings.Contains(y, "name: api-1") || !strings.Contains(y, "nodeName: w1") {
		t.Fatalf("%q %v", y, err)
	}
	if !strings.HasSuffix(fake.last(), "kubectl get pod api-1 -o yaml -n shop --context prod --request-timeout=15s") {
		t.Fatalf("%q", fake.last())
	}
	// a node is cluster scoped: no -n
	if _, err := h.app.Server.K8sYAML(sid, "prod", "node", "", "worker-1"); err != nil || strings.Contains(fake.last(), " -n ") {
		t.Fatalf("%v %q", err, fake.last())
	}
}

// ---- detecting kubectl and the kubeconfig, by running the real scripts against stand-in programs ----

func k8sShell(t *testing.T) string {
	t.Helper()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	return sh
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
}

// runLocal runs a script with sh, with the given home folder and a PATH of the given folder only (sh builtins do the rest).
func runLocal(t *testing.T, script, home, path string, env ...string) string {
	t.Helper()
	cmd := exec.Command(k8sShell(t), "-c", script)
	cmd.Env = append([]string{"HOME=" + home, "PATH=" + path}, env...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func TestK8sDetectFindsKubectlK3sMicroK8sAndTheFixedFolders(t *testing.T) {
	version := `echo "Client Version: v1.29.2"; echo "Kustomize Version: v5.0.4"`
	_, haveLocalBin := os.Stat("/usr/local/bin/kubectl")
	t.Run("kubectl", func(t *testing.T) {
		dir := t.TempDir()
		writeExec(t, filepath.Join(dir, "kubectl"), version)
		res, inDir := parseDetect(runLocal(t, k8sDetectScript, t.TempDir(), dir))
		if !res.Found || res.Cmd != "kubectl" || res.Path != filepath.Join(dir, "kubectl") || res.ClientVersion != "v1.29.2" || inDir || len(res.Tried) != 1 {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("k3s", func(t *testing.T) {
		dir := t.TempDir()
		writeExec(t, filepath.Join(dir, "k3s"), `[ "$1" = kubectl ] && echo "Client Version: v1.28.5+k3s1"`)
		res, _ := parseDetect(runLocal(t, k8sDetectScript, t.TempDir(), dir))
		if !res.Found || res.Cmd != "k3s kubectl" || res.ClientVersion != "v1.28.5+k3s1" || strings.Join(res.Tried, "|") != "kubectl|k3s kubectl" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("microk8s", func(t *testing.T) {
		dir := t.TempDir()
		writeExec(t, filepath.Join(dir, "microk8s"), `[ "$1" = kubectl ] && echo "Client Version: v1.30.1"`)
		res, _ := parseDetect(runLocal(t, k8sDetectScript, t.TempDir(), dir))
		if !res.Found || res.Cmd != "microk8s kubectl" || res.ClientVersion != "v1.30.1" || len(res.Tried) != 3 {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("a program that fails is not kubectl", func(t *testing.T) {
		dir := t.TempDir()
		writeExec(t, filepath.Join(dir, "k3s"), `echo "k3s: permission denied" >&2; exit 1`)
		if haveLocalBin == nil {
			t.Skip("this machine has /usr/local/bin/kubectl")
		}
		if res, _ := parseDetect(runLocal(t, k8sDetectScript, t.TempDir(), dir)); res.Found || len(res.Tried) != 5 || res.Tried[3] != "/usr/local/bin/kubectl" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("home folder", func(t *testing.T) {
		if haveLocalBin == nil {
			t.Skip("this machine has /usr/local/bin/kubectl")
		}
		home := t.TempDir()
		writeExec(t, filepath.Join(home, ".local/bin/kubectl"), version)
		res, inDir := parseDetect(runLocal(t, k8sDetectScript, home, t.TempDir()))
		if !res.Found || !inDir || res.Cmd != "kubectl" || res.Path != filepath.Join(home, ".local/bin/kubectl") {
			t.Fatalf("%+v", res)
		}
	})
}

func TestK8sDetectRemembersTheWayInPerSession(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "@@@try"):
			return "@@@try kubectl\n@@@try k3s kubectl\n@@@found k3s kubectl\n@@@path /usr/local/bin/k3s\n@@@version\nClient Version: v1.28.5+k3s1\n", "", 0
		}
		return "", "", 0
	})
	d, err := h.app.Server.K8sDetect(sid)
	if err != nil || !d.Found || d.Cmd != "k3s kubectl" {
		t.Fatalf("%+v %v", d, err)
	}
	if _, err := h.app.Server.K8sPods(sid, "default", "kube-system"); err != nil {
		t.Fatal(err)
	}
	if c := fake.find("get pods"); len(c) != 1 || !strings.HasSuffix(c[0], "k3s kubectl get pods -o json -n kube-system --context default --request-timeout=15s") {
		t.Fatalf("%q", c)
	}
	// a path the host printed is data: one that is odd is not used
	sid2, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "@@@try x\n@@@found kubectl\n@@@dir\n@@@path /home/u/.local/bin/kube ctl;id\n@@@version\nv1.1.1\n", "", 0
	})
	if d, err := h.app.Server.K8sDetect(sid2); err != nil || d.Found {
		t.Fatalf("%+v %v", d, err)
	}
}

const kubeconfigJSON = `{"current-context":"prod","contexts":[
 {"name":"prod","context":{"cluster":"prod-cluster","user":"admin"}},
 {"name":"arn:aws:eks:eu-1:1:cluster/stage","context":{"cluster":"stage-cluster","user":"eks"}},
 {"name":"bad name;id","context":{"cluster":"x","user":"y"}}],
 "clusters":[{"name":"prod-cluster","cluster":{"server":"https://10.0.0.1:6443"}},{"name":"stage-cluster","cluster":{"server":"https://stage.example.com"}}]}`

func TestK8sContextsSearchRunsAgainstTheFixedPlaces(t *testing.T) {
	stub := func(t *testing.T, body string) string {
		dir := t.TempDir()
		writeExec(t, filepath.Join(dir, "kubectl"), body)
		return dir
	}
	reads := `case "$*" in *"config view -o json"*) cat "$KUBE_JSON";; esac`
	jsonFile := filepath.Join(t.TempDir(), "k.json")
	if err := os.WriteFile(jsonFile, []byte(kubeconfigJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	path := func(dir string) string { return dir + ":/usr/bin:/bin" }
	script := k8sContextsScript("kubectl")

	t.Run("default kubeconfig", func(t *testing.T) {
		home := t.TempDir()
		_ = os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
		_ = os.WriteFile(filepath.Join(home, ".kube/config"), []byte("x"), 0o600)
		res, id, _ := parseContexts(runLocal(t, script, home, path(stub(t, reads)), "KUBE_JSON="+jsonFile))
		if res.Kind != "" || id != "" || res.Sudo || res.Kubeconfig != filepath.Join(home, ".kube/config") || len(res.Contexts) != 2 {
			t.Fatalf("%+v %q", res, id)
		}
		if c := res.Contexts[0]; c.Name != "prod" || !c.Current || c.Server != "https://10.0.0.1:6443" || c.Cluster != "prod-cluster" || c.User != "admin" {
			t.Fatalf("%+v", c)
		}
		if res.Contexts[1].Current || res.Contexts[1].Name != "arn:aws:eks:eu-1:1:cluster/stage" {
			t.Fatalf("%+v", res.Contexts[1])
		}
	})
	t.Run("KUBECONFIG wins", func(t *testing.T) {
		home := t.TempDir()
		res, id, _ := parseContexts(runLocal(t, script, home, path(stub(t, reads)), "KUBE_JSON="+jsonFile, "KUBECONFIG=/srv/kube/prod.yaml"))
		if res.Kind != "" || id != "" || res.Kubeconfig != "/srv/kube/prod.yaml" {
			t.Fatalf("%+v %q", res, id)
		}
	})
	t.Run("a broken KUBECONFIG falls back to the home file, named explicitly", func(t *testing.T) {
		home := t.TempDir()
		_ = os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
		_ = os.WriteFile(filepath.Join(home, ".kube/config"), []byte("x"), 0o600)
		body := `case "$*" in *"--kubeconfig="*) cat "$KUBE_JSON";; *) echo "error loading config file: permission denied" >&2; exit 1;; esac`
		res, id, _ := parseContexts(runLocal(t, script, home, path(stub(t, body)), "KUBE_JSON="+jsonFile, "KUBECONFIG=/root/x"))
		if res.Kind != "" || id != "home" || res.Kubeconfig != filepath.Join(home, ".kube/config") {
			t.Fatalf("%+v %q", res, id)
		}
	})
	t.Run("nothing found", func(t *testing.T) {
		if _, err := os.Stat("/etc/rancher/k3s/k3s.yaml"); err == nil {
			t.Skip("a k3s here")
		}
		if _, err := os.Stat("/etc/kubernetes/admin.conf"); err == nil {
			t.Skip("a kubeadm here")
		}
		res, id, _ := parseContexts(runLocal(t, script, t.TempDir(), path(stub(t, reads))))
		if res.Kind != "kubeconfig" || res.Raw != k8sNoKubeconfig || id != "" || res.Contexts == nil || len(res.Contexts) != 0 {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("a kubeconfig that cannot be read says why", func(t *testing.T) {
		home := t.TempDir()
		_ = os.MkdirAll(filepath.Join(home, ".kube"), 0o755)
		_ = os.WriteFile(filepath.Join(home, ".kube/config"), []byte("x"), 0o600)
		body := `echo 'error loading config file "/home/u/.kube/config": open: permission denied' >&2; exit 1`
		res, _, _ := parseContexts(runLocal(t, script, home, path(stub(t, body))))
		if res.Kind != "kubeconfig" || !strings.Contains(res.Raw, "permission denied") || !k8sKubeconfigProblem(res.Raw) {
			t.Fatalf("%+v", res)
		}
	})
}

func TestK8sContextsWithRootOnlyKubeconfigs(t *testing.T) {
	h := newHarness(t).unlocked(t)
	cases := []struct {
		name, label, path string
		wantID, wantCmd   string
	}{
		{"k3s", "k3s", "/etc/rancher/k3s/k3s.yaml", "k3s", "$SUDO kubectl get pods -o json -n kube-system --context default --kubeconfig=/etc/rancher/k3s/k3s.yaml --request-timeout=15s"},
		{"kubeadm", "admin", "/etc/kubernetes/admin.conf", "admin", "$SUDO kubectl get pods -o json -n kube-system --context default --kubeconfig=/etc/kubernetes/admin.conf --request-timeout=15s"},
	}
	for _, c := range cases {
		sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
			if strings.Contains(cmd, "config view") {
				return "@@@sudo 1\n@@@err k3s\nsomething\n@@@path " + c.path + "\n@@@src " + c.label + " 1\n" + kubeconfigJSON + "\n", "", 0
			}
			return `{"items":[]}`, "", 0
		})
		res, err := h.app.Server.K8sContexts(sid)
		if err != nil || !res.Sudo || res.Kubeconfig != c.path || len(res.Contexts) != 2 {
			t.Fatalf("%s: %+v %v", c.name, res, err)
		}
		if _, err := h.app.Server.K8sPods(sid, "default", "kube-system"); err != nil {
			t.Fatal(err)
		}
		got := fake.find("get pods")[0]
		if !strings.HasSuffix(got, c.wantCmd) || !strings.Contains(got, `SUDO="sudo -n"`) {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	// no passwordless sudo: the reason, with the kind that sends the screen to its kubeconfig step
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "@@@sudo 1\n@@@err k3s\n/etc/rancher/k3s/k3s.yaml can only be read by root, and this login has no passwordless sudo.\n", "", 0
	})
	res, err := h.app.Server.K8sContexts(sid)
	if err != nil || res.Kind != "kubeconfig" || !strings.Contains(res.Raw, "passwordless sudo") {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestK8sScriptsAreValidShell(t *testing.T) {
	sh := k8sShell(t)
	st := &k8sState{kubeconfig: "k3s", sudo: true, prefix: "k3s kubectl"}
	scripts := map[string]string{
		"detect": k8sDetectScript, "contexts": k8sContextsScript("kubectl"), "contexts k3s": k8sContextsScript("k3s kubectl"),
		"forward": k8sForwardScript(st, "prod", "shop", "web", 18001, 80), "forward plain": k8sForwardScript(&k8sState{}, "arn:aws:eks:eu-1:1:cluster/p", "shop", "web", 18001, 80),
		"line": st.wrap(st.line(kcmd{ctx: "c", ns: "n", args: []string{"get", "pods", "-o", `go-template={{range .items}}{{"\n"}}{{end}}`}, read: true})),
	}
	for name, script := range scripts {
		if out, err := exec.Command(sh, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Errorf("%s: %v\n%s", name, err, out)
		}
	}
}

// ---- port-forward ----

// The script of the remote side: kubectl is stood in for by a program that prints the line and waits; closing the standard input
// of the script (what the panel does on Stop, and sshd does when the connection drops) must kill it.
func TestK8sForwardScriptKillsKubectlWhenItsInputEnds(t *testing.T) {
	sh := k8sShell(t)
	dir, marker := t.TempDir(), filepath.Join(t.TempDir(), "killed")
	writeExec(t, filepath.Join(dir, "kubectl"), `echo "Forwarding from 127.0.0.1:18001 -> 80"
trap 'echo killed > "$K8S_MARKER"; exit 0' TERM
while :; do sleep 0.1; done`)
	script := k8sForwardScript(&k8sState{}, "prod", "shop", "web", 18001, 80)
	cmd := exec.Command(sh, "-c", script)
	cmd.Env = []string{"PATH=" + dir + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "K8S_MARKER=" + marker}
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || !strings.Contains(line, "Forwarding from 127.0.0.1:18001") {
		t.Fatalf("%q %v", line, err)
	}
	time.Sleep(300 * time.Millisecond) // it must still be there while the input is open
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("kubectl was killed while the input was open")
	}
	_ = stdin.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the script did not end when its input ended")
	}
	if b, _ := os.ReadFile(marker); !strings.Contains(string(b), "killed") {
		t.Fatalf("kubectl was not told to stop: %q", b)
	}
}

func TestFwdOutSeesTheReadyLine(t *testing.T) {
	w := &fwdOut{want: "Forwarding from 127.0.0.1:18123", ready: make(chan struct{})}
	_, _ = w.Write([]byte("Forwarding from [::1]:18123 -> 80\r\n"))
	select {
	case <-w.ready:
		t.Fatal("another address is not ready")
	default:
	}
	_, _ = w.Write([]byte("Forwarding from 127.0.0.1:"))
	_, _ = w.Write([]byte("18123 -> 80\n"))
	select {
	case <-w.ready:
	default:
		t.Fatal("a line that came in pieces was missed")
	}
	if !strings.Contains(w.text(), "18123 -> 80") {
		t.Fatal(w.text())
	}
}

func TestK8sPipeCopiesBothWays(t *testing.T) {
	a1, a2 := net.Pipe()
	b1, b2 := net.Pipe()
	go k8sPipe(a2, b1)
	go func() { _, _ = a1.Write([]byte("ping")) }()
	buf := make([]byte, 4)
	if _, err := io.ReadFull(b2, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("%q %v", buf, err)
	}
	go func() { _, _ = b2.Write([]byte("pong")) }()
	if _, err := io.ReadFull(a1, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("%q %v", buf, err)
	}
	_ = a1.Close()
	_ = b2.Close()
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// A port-forward end to end on the test SSH server: the host side is a fake kubectl that prints its ready line and runs an echo
// server on the port it was given, until its standard input ends.
func TestK8sPortForwardThroughTheSSHConnection(t *testing.T) {
	h := newHarness(t).unlocked(t)
	var mu sync.Mutex
	var cmds []string
	ended := make(chan string, 4)
	portRe := regexp.MustCompile(`svc/web (\d+):80 --address 127\.0\.0\.1`)
	srv, err := testssh.Start(testssh.Config{User: "root", Password: "pw",
		ExecFunc:    func(cmd string) (string, string, int) { return "", "", 0 },
		StreamMatch: func(cmd string) bool { return strings.Contains(cmd, "port-forward") },
		ExecStream: func(cmd string, stdin io.Reader, stdout, stderr io.Writer) int {
			mu.Lock()
			cmds = append(cmds, cmd)
			mu.Unlock()
			m := portRe.FindStringSubmatch(cmd)
			if m == nil {
				_, _ = io.WriteString(stderr, "error: bad command\n")
				return 1
			}
			if m[1] == "18000" { // never chosen by a test, just to be sure the pattern is the one sent
				return 1
			}
			l, err := net.Listen("tcp", "127.0.0.1:"+m[1])
			if err != nil {
				_, _ = io.WriteString(stderr, "Unable to listen on port "+m[1]+": address already in use\n")
				return 1
			}
			defer l.Close()
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
				}
			}()
			_, _ = io.WriteString(stdout, "Forwarding from 127.0.0.1:"+m[1]+" -> 80\r\nForwarding from [::1]:"+m[1]+" -> 80\r\n")
			_, _ = io.Copy(io.Discard, stdin) // until the panel closes it
			ended <- m[1]
			return 0
		}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	sid := h.openServer(t, srv)

	local := freePort(t)
	f, err := h.app.Server.K8sPortForwardStart(sid, "prod", "shop", "web", local, 80)
	if err != nil {
		t.Fatal(err)
	}
	if f.Status != "running" || f.LocalPort != local || f.RemotePort != 80 || f.Service != "web" || f.Namespace != "shop" || f.ID == "" {
		t.Fatalf("%+v", f)
	}
	mu.Lock()
	sent := cmds[0]
	mu.Unlock()
	if !strings.Contains(sent, "kubectl port-forward svc/web ") || !strings.Contains(sent, "--address 127.0.0.1 -n shop --context prod") {
		t.Fatalf("%q", sent)
	}
	hp, _ := strconv.Atoi(portRe.FindStringSubmatch(sent)[1])
	if hp < 18000 || hp > 18999 {
		t.Fatalf("the host port %d is not in 18000-18999", hp)
	}
	// the local side listens on 127.0.0.1 only
	ln, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", local))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = ln.Write([]byte("hello through ssh"))
	buf := make([]byte, len("hello through ssh"))
	_ = ln.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(ln, buf); err != nil || string(buf) != "hello through ssh" {
		t.Fatalf("%q %v", buf, err)
	}
	_ = ln.Close()
	if a := fmt.Sprint(f); strings.Contains(a, "0.0.0.0") {
		t.Fatal(a)
	}
	// a port that is taken here is refused with a message, and nothing is started
	if _, err := h.app.Server.K8sPortForwardStart(sid, "prod", "shop", "web", local, 80); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("%v", err)
	}
	list, err := h.app.Server.K8sPortForwards(sid)
	if err != nil || len(list) != 1 || list[0].Status != "running" {
		t.Fatalf("%+v %v", list, err)
	}
	if err := h.app.Server.K8sPortForwardStop(sid, f.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("the remote command was not ended by Stop")
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", local), time.Second); err == nil {
		c.Close()
		t.Fatal("the local port is still open after Stop")
	}
	if list, _ = h.app.Server.K8sPortForwards(sid); list[0].Status != "stopped" {
		t.Fatalf("%+v", list)
	}
	if err := h.app.Server.K8sPortForwardStop(sid, "nope"); err == nil {
		t.Fatal("an unknown id")
	}
	// closing the session stops what is still open
	local2 := freePort(t)
	if _, err := h.app.Server.K8sPortForwardStart(sid, "prod", "shop", "web", local2, 80); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Server.Close(sid); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(5 * time.Second):
		t.Fatal("closing the session left the remote command running")
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", local2), 200*time.Millisecond)
		if err != nil {
			break
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("the local port is still open after the session closed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	var started, stopped bool
	for _, e := range h.logs(t, domain.ActivityFilter{}) {
		started = started || strings.Contains(e.Message, fmt.Sprintf("started port-forward svc/web:80 on 127.0.0.1:%d in shop (prod)", local))
		stopped = stopped || strings.Contains(e.Message, "stopped port-forward svc/web")
	}
	if !started || !stopped {
		t.Fatalf("audit: started %v stopped %v", started, stopped)
	}
}

func TestK8sPortForwardReportsAHostThatCannotStartIt(t *testing.T) {
	h := newHarness(t).unlocked(t)
	// the plain fake answers the long command at once with kubectl's complaint
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		return "", "error: services \"web\" not found\n", 1
	})
	local := freePort(t)
	_, err := h.app.Server.K8sPortForwardStart(sid, "prod", "shop", "web", local, 80)
	if err == nil || !strings.Contains(err.Error(), `services "web" not found`) {
		t.Fatalf("%v", err)
	}
	if c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", local), 300*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("a failed start left the local port open")
	}
	if list, _ := h.app.Server.K8sPortForwards(sid); len(list) != 0 {
		t.Fatalf("%+v", list)
	}
}
