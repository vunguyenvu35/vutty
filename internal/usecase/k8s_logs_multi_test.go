package usecase

import (
	"strings"
	"testing"
)

func TestK8sSelectorOfWorkloadsAndServices(t *testing.T) {
	got, err := k8sSelectorOf("deployment", []byte(`{"spec":{"selector":{"matchLabels":{"b":"2","a":"1"},"matchExpressions":[
	  {"key":"tier","operator":"In","values":["x","y"]},{"key":"env","operator":"NotIn","values":["dev"]},{"key":"canary","operator":"DoesNotExist"},{"key":"app","operator":"Exists"}]}}}`))
	if err != nil || got != "!canary,a=1,app,b=2,env notin (dev),tier in (x,y)" {
		t.Fatalf("%q, %v", got, err)
	}
	if got, err := k8sSelectorOf("service", []byte(`{"spec":{"selector":{"app":"web","tier":"fe"}}}`)); err != nil || got != "app=web,tier=fe" {
		t.Fatalf("%q, %v", got, err)
	}
	for _, bad := range []struct{ kind, doc string }{
		{"service", `{"spec":{}}`}, {"deployment", `{"spec":{"selector":{}}}`}, {"service", `nonsense`},
		{"deployment", `{"spec":{"selector":{"matchExpressions":[{"key":"a","operator":"Weird"}]}}}`},
	} {
		if _, err := k8sSelectorOf(bad.kind, []byte(bad.doc)); err == nil {
			t.Errorf("%s %s was accepted", bad.kind, bad.doc)
		}
	}
}

func TestK8sLogsMultiMergesThePodsByTime(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get deployment api -o json"):
			return `{"spec":{"selector":{"matchLabels":{"app":"api"},"matchExpressions":[{"key":"tier","operator":"In","values":["a","b"]}]}}}`, "", 0
		case strings.Contains(cmd, "get pods -l"):
			return `{"items":[
			  {"metadata":{"name":"api-2"},"spec":{"containers":[{"name":"api"},{"name":"proxy"}]},"status":{"phase":"Running"}},
			  {"metadata":{"name":"api-1"},"spec":{"containers":[{"name":"api"}]},"status":{"phase":"Running"}},
			  {"metadata":{"name":"api-3"},"spec":{"containers":[{"name":"api"}]},"status":{"phase":"Pending"}},
			  {"metadata":{"name":"api-4"},"spec":{"containers":[{"name":"api"}]},"status":{"phase":"Running"}}]}`, "", 0
		case strings.Contains(cmd, "kubectl logs api-1 "):
			return "2026-11-04T10:00:01.000000000Z one-a\n2026-11-04T10:00:04Z one-b\ncontinued line\n", "", 0
		case strings.Contains(cmd, "kubectl logs api-2 ") && strings.Contains(cmd, "-c api "):
			return "2026-11-04T10:00:02.500000000Z two-a\n", "", 0
		case strings.Contains(cmd, "kubectl logs api-2 ") && strings.Contains(cmd, "-c proxy "):
			return "2026-11-04T10:00:03.000000000Z two-proxy\n", "", 0
		case strings.Contains(cmd, "kubectl logs api-4 "):
			return "", "container api is waiting to start: CrashLoopBackOff", 1
		}
		return "", "", 0
	})
	out, err := h.app.Server.K8sLogsMulti(sid, "prod", "shop", "deployment", "api", K8sLogOpts{Lines: 300, Since: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	want := "[api-1] one-a\n[api-2/api] two-a\n[api-2/proxy] two-proxy\n[api-1] one-b\n[api-1] continued line\n"
	if out.Text != want || out.Pods != 3 || out.Skipped != 0 {
		t.Fatalf("%+v", out)
	}
	if len(out.Failed) != 1 || !strings.HasPrefix(out.Failed[0], "api-4: ") || !strings.Contains(out.Failed[0], "CrashLoopBackOff") {
		t.Fatalf("failed = %q", out.Failed)
	}
	if c := fake.find("get pods -l"); len(c) != 1 || !strings.Contains(c[0], "'app=api,tier in (a,b)'") {
		t.Fatalf("the selector is one quoted word: %q", c)
	}
	if c := fake.find("kubectl logs api-1 "); len(c) != 1 || !strings.HasSuffix(c[0], "kubectl logs api-1 --tail=300 --timestamps -c api --since=1h -n shop --context prod --request-timeout=15s") {
		t.Fatalf("%q", c)
	}
	if fake.find("kubectl logs api-3 ") != nil {
		t.Fatal("the log of a pod that is pending was asked for")
	}

	// with timestamps the lines keep them; the newest are kept when there are more than asked for
	one, err := h.app.Server.K8sLogsMulti(sid, "prod", "shop", "deployment", "api", K8sLogOpts{Lines: 100, Timestamps: true, Container: "proxy"})
	if err != nil || one.Text != "[api-2] 2026-11-04T10:00:03.000000000Z two-proxy\n" {
		t.Fatalf("%+v, %v", one, err)
	}
}

func TestK8sLogsMultiRefusesBadInputAndLimitsPods(t *testing.T) {
	h := newHarness(t).unlocked(t)
	s := h.app.Server
	for _, bad := range k8sBadWords {
		if _, err := s.K8sLogsMulti("x", "c", "ns", "deployment", bad, K8sLogOpts{Lines: 100}); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("name %q: %v", bad, err)
		}
		if _, err := s.K8sLogsMulti("x", "c", bad, "service", "n", K8sLogOpts{Lines: 100}); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("namespace %q: %v", bad, err)
		}
	}
	for _, o := range []struct {
		kind string
		opts K8sLogOpts
	}{{"pod", K8sLogOpts{Lines: 100}}, {"secret", K8sLogOpts{Lines: 100}}, {"deployment", K8sLogOpts{Lines: 7}}, {"deployment", K8sLogOpts{Lines: 100, Since: "9y"}}, {"deployment", K8sLogOpts{Lines: 100, Container: "a b"}}} {
		if _, err := s.K8sLogsMulti("x", "c", "ns", o.kind, "n", o.opts); err == nil || strings.Contains(err.Error(), "connection to the server is closed") {
			t.Errorf("%+v was not refused: %v", o, err)
		}
	}
	var pods []string
	for i := 0; i < 25; i++ {
		pods = append(pods, `{"metadata":{"name":"p`+string(rune('a'+i))+`"},"spec":{"containers":[{"name":"c"}]},"status":{"phase":"Running"}}`)
	}
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get service web -o json"):
			return `{"spec":{"selector":{"app":"web"}}}`, "", 0
		case strings.Contains(cmd, "get pods -l"):
			return `{"items":[` + strings.Join(pods, ",") + `]}`, "", 0
		}
		return "2026-11-04T10:00:00Z hello\n", "", 0
	})
	out, err := s.K8sLogsMulti(sid, "prod", "shop", "service", "web", K8sLogOpts{Lines: 100})
	if err != nil || out.Pods != k8sMaxLogPods || out.Skipped != 5 || strings.Count(out.Text, "\n") != 20 {
		t.Fatalf("%d pods, %d skipped, %v", out.Pods, out.Skipped, err)
	}
}
