package usecase

import (
	"strings"
	"testing"
)

const secPods = `{"items":[
 {"metadata":{"name":"web-7d9-aaa","namespace":"shop","labels":{"pod-template-hash":"7d9"},"ownerReferences":[{"kind":"ReplicaSet","name":"web-7d9"}]},
  "spec":{"hostNetwork":true,"volumes":[{"name":"sock","hostPath":{"path":"/var/run/docker.sock"}},{"name":"data","hostPath":{"path":"/mnt/data"}}],
   "containers":[{"name":"app","image":"nginx","env":[{"name":"DB_PASSWORD","value":"hunter2"},{"name":"API_TOKEN","valueFrom":{"secretKeyRef":{"name":"s","key":"k"}}},{"name":"MODE","value":"x"}],
    "securityContext":{"privileged":true,"capabilities":{"add":["net_admin","CHOWN"]}}}]}},
 {"metadata":{"name":"web-7d9-bbb","namespace":"shop","labels":{"pod-template-hash":"7d9"},"ownerReferences":[{"kind":"ReplicaSet","name":"web-7d9"}]},
  "spec":{"hostNetwork":true,"containers":[{"name":"app","image":"nginx:1.25"}]}},
 {"metadata":{"name":"db-0","namespace":"data","ownerReferences":[{"kind":"StatefulSet","name":"db"}]},
  "spec":{"securityContext":{"runAsUser":0},"containers":[{"name":"pg","image":"postgres@sha256:abc"}]}}
]}`

func findRule(fs []K8sFinding, rule string) []K8sFinding {
	var out []K8sFinding
	for _, f := range fs {
		if f.Rule == rule {
			out = append(out, f)
		}
	}
	return out
}

func TestSecurityPodsFindsWhatIsExposedAndNamesTheWorkloadOnce(t *testing.T) {
	fs, n, err := securityPods(secPods)
	if err != nil || n != 3 {
		t.Fatalf("%d %v", n, err)
	}
	if h := findRule(fs, "host-namespace"); len(h) != 1 || h[0].Kind != "Deployment" || h[0].Name != "web" || h[0].Count != 2 || h[0].Severity != "high" {
		t.Fatalf("two replicas are one finding of the Deployment: %+v", h)
	}
	if p := findRule(fs, "host-path"); len(p) != 2 {
		t.Fatalf("%+v", p)
	} else {
		sev := map[string]string{}
		for _, f := range p {
			sev[f.Detail] = f.Severity
		}
		if sev["mounts /var/run/docker.sock of the node"] != "high" || sev["mounts /mnt/data of the node"] != "medium" {
			t.Fatalf("%v", sev)
		}
	}
	if c := findRule(fs, "capability"); len(c) != 1 || !strings.Contains(c[0].Detail, "NET_ADMIN") || strings.Contains(c[0].Detail, "CHOWN") {
		t.Fatalf("%+v", c)
	}
	if len(findRule(fs, "privileged")) != 1 {
		t.Fatal("privileged")
	}
	if r := findRule(fs, "run-as-root"); len(r) != 1 || r[0].Name != "db" || r[0].Kind != "StatefulSet" {
		t.Fatalf("%+v", r)
	}
	s := findRule(fs, "plain-secret")
	if len(s) != 1 || !strings.Contains(s[0].Detail, "DB_PASSWORD") || strings.Contains(s[0].Detail, "API_TOKEN") || strings.Contains(s[0].Detail, "MODE") {
		t.Fatalf("only a plain value is a finding: %+v", s)
	}
	for _, f := range fs {
		if strings.Contains(f.Detail, "hunter2") {
			t.Fatalf("a value is shown: %+v", f)
		}
	}
	if im := findRule(fs, "image-tag"); len(im) != 1 || im[0].Name != "web" || !strings.Contains(im[0].Detail, "nginx") {
		t.Fatalf("a pinned image and a tag are fine, no tag is not: %+v", im)
	}
}

func TestSecurityNetworkFindsWhatIsReachableFromOutside(t *testing.T) {
	svc := `{"items":[
	 {"metadata":{"name":"a","namespace":"x"},"spec":{"type":"NodePort","ports":[{"port":80,"nodePort":30080}]}},
	 {"metadata":{"name":"b","namespace":"x"},"spec":{"type":"LoadBalancer","ports":[{"port":443}]}},
	 {"metadata":{"name":"c","namespace":"x"},"spec":{"type":"ClusterIP","ports":[{"port":80}]}},
	 {"metadata":{"name":"c","namespace":"x"},"subsets":[]}]}`
	ing := `{"items":[{"metadata":{"name":"i","namespace":"x"},"spec":{"rules":[{"host":"a.example.com"}]}},{"metadata":{"name":"j","namespace":"x"},"spec":{"tls":[{"hosts":["b"]}],"rules":[{"host":"b"}]}}]}`
	fs, n, err := securityNetwork(svc, ing)
	if err != nil || n != 3 || len(fs) != 3 {
		t.Fatalf("%d %+v %v", n, fs, err)
	}
	if f := findRule(fs, "node-port"); len(f) != 1 || !strings.Contains(f[0].Detail, "30080") {
		t.Fatalf("%+v", f)
	}
	if f := findRule(fs, "ingress-no-tls"); len(f) != 1 || f[0].Name != "i" || !strings.Contains(f[0].Detail, "a.example.com") {
		t.Fatalf("%+v", f)
	}
	if _, _, err := securityNetwork("not json", ""); err == nil {
		t.Fatal("garbage")
	}
}

func TestK8sSecurityOnAHostSortsHighFirstAndRefusesBadInput(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, _ := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get pods"):
			return secPods, "", 0
		case strings.Contains(cmd, "get services"):
			return `{"items":[{"metadata":{"name":"a","namespace":"x"},"spec":{"type":"NodePort","ports":[{"port":80,"nodePort":30080}]}}]}`, "", 0
		}
		return `{"items":[]}`, "", 0
	})
	r, err := h.app.Server.K8sSecurity(sid, "prod", "")
	if err != nil || r.Pods != 3 || r.Services != 1 || len(r.Findings) == 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Findings[0].Severity != "high" || r.Findings[len(r.Findings)-1].Severity != "low" {
		t.Fatalf("not sorted by severity: %+v", r.Findings)
	}
	for _, bad := range k8sBadWords {
		if _, err := h.app.Server.K8sSecurity(sid, bad, ""); err == nil {
			t.Errorf("context %q", bad)
		}
		if _, err := h.app.Server.K8sSecurity(sid, "prod", bad); err == nil && bad != "" {
			t.Errorf("namespace %q", bad)
		}
	}
}
