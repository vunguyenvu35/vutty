package usecase

import (
	"encoding/json"
	"strings"
	"testing"
)

// what a cluster with cert-manager answers to discovery and to a list of its objects
var discoveryFixtures = map[string]string{
	"/api/v1": `{"resources":[
	  {"name":"pods","kind":"Pod","namespaced":true,"verbs":["get","list"]},
	  {"name":"pods/log","kind":"Pod","namespaced":true,"verbs":["get"]},
	  {"name":"secrets","kind":"Secret","namespaced":true,"verbs":["get","list"]},
	  {"name":"bindings","kind":"Binding","namespaced":true,"verbs":["create"]},
	  {"name":"nodes","kind":"Node","namespaced":false,"verbs":["get","list"]}]}`,
	"/apis":                    `{"groups":[{"name":"cert-manager.io","preferredVersion":{"version":"v1"}},{"name":"broken.example.com","preferredVersion":{"version":"v1"}}]}`,
	"/apis/cert-manager.io/v1": `{"resources":[{"name":"certificates","kind":"Certificate","namespaced":true,"verbs":["get","list"],"shortNames":["cert"]}]}`,
	"/apis/apiextensions.k8s.io/v1/customresourcedefinitions/certificates.cert-manager.io": `{"spec":{"versions":[
	  {"name":"v1alpha2","additionalPrinterColumns":[{"name":"Old","jsonPath":".spec.old"}]},
	  {"name":"v1","additionalPrinterColumns":[
	    {"name":"Ready","jsonPath":".status.conditions[?(@.type==\"Ready\")].status"},
	    {"name":"Secret","jsonPath":".spec.secretName"},
	    {"name":"Age","jsonPath":".metadata.creationTimestamp"},
	    {"name":"Issuer","jsonPath":".spec.issuerRef.name","priority":1}]}]}}`,
	"/apis/cert-manager.io/v1/namespaces/shop/certificates?limit=200": `{"metadata":{"continue":"abc"},"items":[
	  {"metadata":{"name":"web","namespace":"shop","creationTimestamp":"2026-11-01T00:00:00Z"},"spec":{"secretName":"web-tls"},
	   "status":{"conditions":[{"type":"Issued","status":"False"},{"type":"Ready","status":"True"}]}},
	  {"metadata":{"name":"api","namespace":"shop","creationTimestamp":"2026-11-02T00:00:00Z"},"spec":{"secretName":"api-tls"}}]}`,
	"/apis/cert-manager.io/v1/namespaces/shop/certificates/web": `{"apiVersion":"cert-manager.io/v1","kind":"Certificate","metadata":{"name":"web","namespace":"shop","managedFields":[{"manager":"x"}]},"spec":{"secretName":"web-tls"}}`,
}

func TestJSONPathValueReadsWhatPrinterColumnsUse(t *testing.T) {
	var obj any
	obj = map[string]any{
		"spec": map[string]any{"replicas": float64(3), "paused": true, "ports": []any{float64(80), float64(443)}, "sel": map[string]any{"a": "b"}},
		"status": map[string]any{"conditions": []any{
			map[string]any{"type": "Issued", "status": "False"}, map[string]any{"type": "Ready", "status": "True"}}},
	}
	for path, want := range map[string]string{
		".spec.replicas": "3",
		".spec.paused":   "true",
		".spec.ports[1]": "443",
		".spec.ports[*]": "80,443",
		".spec.sel":      `{"a":"b"}`,
		".spec.missing":  "",
		".spec.ports[9]": "",
		".status.conditions[?(@.type==\"Ready\")].status": "True",
		".status.conditions[?(@.type==\"Gone\")].status":  "",
		".status.conditions[?(@.type":                     "",
		"spec":                                            "",
	} {
		if got := jsonPathValue(obj, path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}

func TestClusterListsAnyKindAndItsObjects(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server

	all, err := s.K8sResources(sid, "prod")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]K8sAPIResource{}
	for _, r := range all {
		have[r.Kind] = r
	}
	if c := have["Certificate"]; c.Group != "cert-manager.io" || c.Version != "v1" || c.Plural != "certificates" || !c.Namespaced || len(c.Short) != 1 {
		t.Fatalf("certificate = %+v", c)
	}
	if n := have["Node"]; n.Namespaced || n.Plural != "nodes" {
		t.Fatalf("node = %+v", n)
	}
	if _, ok := have["Secret"]; ok {
		t.Fatal("the Secrets of the core group are not offered")
	}
	if _, ok := have["Binding"]; ok || len(all) != 3 {
		t.Fatalf("what cannot be listed is left out, and so is a group that does not answer: %+v", all)
	}
	if _, err := s.K8sResources(sid, "prod"); err != nil || len(f.called("GET", "/api/v1")) != 1 {
		t.Fatalf("discovery is kept for a while: %v", f.calls)
	}

	cert := K8sResRef{Group: "cert-manager.io", Version: "v1", Plural: "certificates"}
	list, err := s.K8sObjects(sid, "prod", "shop", cert)
	if err != nil {
		t.Fatal(err)
	}
	if !list.More || strings.Join(list.Columns, ",") != "Ready,Secret" || len(list.Rows) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if r := list.Rows[0]; r.Name != "api" || r.Namespace != "shop" || strings.Join(r.Cells, "|") != "|api-tls" {
		t.Fatalf("row 0 = %+v", r)
	}
	if r := list.Rows[1]; r.Name != "web" || strings.Join(r.Cells, "|") != "True|web-tls" {
		t.Fatalf("row 1 = %+v", r)
	}
	if len(f.called("GET", "/apis/cert-manager.io/v1/namespaces/shop/certificates?limit=200")) != 1 {
		t.Fatalf("calls = %v", f.calls)
	}

	nodes, err := s.K8sObjects(sid, "prod", "shop", K8sResRef{Version: "v1", Plural: "nodes"}) // cluster-wide: no namespace in the path
	if err != nil || !strings.Contains(strings.Join(f.called("GET", "/api/v1/nodes?"), ""), "limit=200") || nodes.Namespaced {
		t.Fatalf("nodes = %+v, %v, %v", nodes, err, f.calls)
	}

	y, err := s.K8sObjectYAML(sid, "prod", "shop", cert, "web")
	if err != nil || !strings.Contains(y, "kind: Certificate") || !strings.Contains(y, "secretName: web-tls") || strings.Contains(y, "managedFields") {
		t.Fatalf("yaml = %q, %v", y, err)
	}
}

func TestClusterNeverOpensASecretThroughTheGenericScreen(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	secrets := K8sResRef{Version: "v1", Plural: "secrets"}
	if _, err := s.K8sObjects(sid, "prod", "shop", secrets); err == nil {
		t.Fatal("secrets were listed")
	}
	if _, err := s.K8sObjectYAML(sid, "prod", "shop", secrets, "api-tls"); err == nil {
		t.Fatal("a secret was shown")
	}
	if got := f.called("GET", "/api/v1/namespaces/shop/secrets"); len(got) != 0 {
		t.Fatalf("the cluster was asked for secrets: %v", got)
	}
}

func TestK8sResourcesRefuseWhatTheClusterDoesNotServe(t *testing.T) {
	h := newHarness(t).unlocked(t)
	f := newFakeCluster(t)
	sid, _ := clusterSession(t, h, f, "{token: tok}")
	s := h.app.Server
	for _, ref := range []K8sResRef{
		{Group: "a b", Version: "v1", Plural: "x"}, {Group: "../x", Version: "v1", Plural: "x"}, {Version: "1", Plural: "pods"},
		{Version: "v1", Plural: "pods/log"}, {Version: "v1", Plural: "Pods"}, {Version: "v1", Plural: "pods;id"}, {Version: "v1", Plural: ""},
		{Group: "cert-manager.io", Version: "v9", Plural: "certificates"}, {Group: "nope.io", Version: "v1", Plural: "things"},
	} {
		if _, err := s.K8sObjects(sid, "prod", "shop", ref); err == nil {
			t.Errorf("%+v was not refused", ref)
		}
		if _, err := s.K8sObjectYAML(sid, "prod", "shop", ref, "x"); err == nil {
			t.Errorf("%+v was not refused for yaml", ref)
		}
	}
	cert := K8sResRef{Group: "cert-manager.io", Version: "v1", Plural: "certificates"}
	for _, bad := range k8sBadWords {
		if _, err := s.K8sObjectYAML(sid, "prod", "shop", cert, bad); err == nil {
			t.Errorf("name %q was not refused", bad)
		}
		if _, err := s.K8sObjectYAML(sid, "prod", bad, cert, "web"); err == nil {
			t.Errorf("namespace %q was not refused", bad)
		}
		if _, err := s.K8sObjects(sid, bad, "shop", cert); err == nil {
			t.Errorf("context %q was not refused", bad)
		}
	}
}

func TestK8sResourcesOnAHostAskTheAPIServerThroughKubectl(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "get --raw /apis/cert-manager.io/v1 --context"):
			return "@@@0\n" + discoveryFixtures["/apis/cert-manager.io/v1"] + "\n@@@1\n", "", 0 // the second group did not answer
		case strings.Contains(cmd, "get --raw /apis --context"):
			return discoveryFixtures["/apis"], "", 0
		case strings.Contains(cmd, "get --raw /api/v1 --context"):
			return discoveryFixtures["/api/v1"], "", 0
		case strings.Contains(cmd, "customresourcedefinitions/certificates.cert-manager.io"):
			return discoveryFixtures["/apis/apiextensions.k8s.io/v1/customresourcedefinitions/certificates.cert-manager.io"], "", 0
		case strings.Contains(cmd, "certificates?limit=200"):
			return discoveryFixtures["/apis/cert-manager.io/v1/namespaces/shop/certificates?limit=200"], "", 0
		case strings.Contains(cmd, "certificates/web"):
			return discoveryFixtures["/apis/cert-manager.io/v1/namespaces/shop/certificates/web"], "", 0
		}
		return "", "", 0
	})
	s := h.app.Server
	all, err := s.K8sResources(sid, "prod")
	if err != nil || len(all) != 3 {
		t.Fatalf("%+v, %v", all, err)
	}
	if got := fake.find("@@@0"); len(got) != 1 || !strings.Contains(got[0], "kubectl get --raw /apis/cert-manager.io/v1 --context prod --request-timeout=15s 2>/dev/null") {
		t.Fatalf("one round trip for the groups: %q", got)
	}
	cert := K8sResRef{Group: "cert-manager.io", Version: "v1", Plural: "certificates"}
	list, err := s.K8sObjects(sid, "prod", "shop", cert)
	if err != nil || len(list.Rows) != 2 || list.Rows[1].Cells[0] != "True" {
		t.Fatalf("%+v, %v", list, err)
	}
	if got := fake.find("certificates?limit=200"); len(got) != 1 || !strings.HasSuffix(got[0], "kubectl get --raw '/apis/cert-manager.io/v1/namespaces/shop/certificates?limit=200' --context prod --request-timeout=15s") {
		t.Fatalf("%q", got)
	}
	if y, err := s.K8sObjectYAML(sid, "prod", "shop", cert, "web"); err != nil || !strings.Contains(y, "kind: Certificate") {
		t.Fatalf("%q, %v", y, err)
	}
}

func TestBuiltinColumnsReadWhatKubectlPrints(t *testing.T) {
	var pv any
	if err := json.Unmarshal([]byte(`{"spec":{"capacity":{"storage":"10Gi"},"accessModes":["ReadWriteOnce","ReadOnlyMany"],"persistentVolumeReclaimPolicy":"Delete",
	  "claimRef":{"namespace":"shop","name":"data-db-0"},"storageClassName":"gp3"},"status":{"phase":"Bound"}}`), &pv); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range k8sBuiltinColumns["/persistentvolumes"] {
		got = append(got, c.name+"="+jsonPathValue(pv, c.path))
	}
	if want := "Capacity=10Gi Access=ReadWriteOnce,ReadOnlyMany Reclaim=Delete Status=Bound Claim=data-db-0 Class=gp3"; strings.Join(got, " ") != want {
		t.Fatalf("%q", strings.Join(got, " "))
	}
	if len(k8sBuiltinColumns["autoscaling/horizontalpodautoscalers"]) != 4 || len(k8sBuiltinColumns["storage.k8s.io/storageclasses"]) != 4 {
		t.Fatal("columns of HPA and StorageClass")
	}
}

func TestK8sCountsAskForOneItemAndAddWhatIsLeft(t *testing.T) {
	h := newHarness(t).unlocked(t)
	sid, fake := h.k8sSession(t, func(cmd string) (string, string, int) {
		switch {
		case strings.Contains(cmd, "/pods?limit=1"):
			return `{"metadata":{"remainingItemCount":194},"items":[{"metadata":{"name":"a"}}]}`, "", 0
		case strings.Contains(cmd, "/deployments?limit=1"):
			return `{"metadata":{"remainingItemCount":182},"items":[{}]}`, "", 0
		case strings.Contains(cmd, "/statefulsets?limit=1"):
			return `{"metadata":{},"items":[{}]}`, "", 0 // the last one: nothing left
		case strings.Contains(cmd, "/daemonsets?limit=1"):
			return `{"metadata":{},"items":[]}`, "", 0
		case strings.Contains(cmd, "/jobs?limit=1"):
			return `{"metadata":{"remainingItemCount":12},"items":[{}]}`, "", 0
		case strings.Contains(cmd, "/cronjobs?limit=1"):
			return `{"metadata":{"remainingItemCount":168},"items":[{}]}`, "", 0
		case strings.Contains(cmd, "/services?limit=1"):
			return `{"metadata":{"remainingItemCount":133},"items":[{}]}`, "", 0
		case strings.Contains(cmd, "/ingresses?limit=1"):
			return "", "ingresses.networking.k8s.io is forbidden", 1 // a refusal: no count for the tab
		}
		return "", "", 0
	})
	got, err := h.app.Server.K8sCounts(sid, "prod", "shop")
	if err != nil {
		t.Fatal(err)
	}
	if got["pods"] != 195 || got["workloads"] != 183+1+0+13+169 {
		t.Fatalf("%v", got)
	}
	if _, ok := got["network"]; ok {
		t.Fatalf("a tab with a kind that was refused has no count: %v", got)
	}
	if c := fake.find("/pods?limit=1"); len(c) != 1 || !strings.Contains(c[0], "kubectl get --raw /api/v1/namespaces/shop/pods?limit=1") && !strings.Contains(c[0], "'/api/v1/namespaces/shop/pods?limit=1'") {
		t.Fatalf("%q", c)
	}
	if _, err := h.app.Server.K8sCounts(sid, "prod", "bad ns"); err == nil {
		t.Fatal("a bad namespace was not refused")
	}
}
