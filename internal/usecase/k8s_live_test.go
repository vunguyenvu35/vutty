package usecase

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// A run against a real cluster (kind or minikube), by both ways in. It is skipped unless VUTTY_LIVE_KUBECONFIG names a kubeconfig, and
// VUTTY_LIVE_CONTEXT its context. The cluster needs, in the namespace shop: a Deployment web-demo, a Helm release `web` (two revisions,
// a Secret in its manifest, the password hunter2 given as a value), and a CRD widgets.example.com with printer columns Size and Phase
// and one Widget w1 (spec.size large, status.phase Ready). See docs/Development.md.

func liveSession(t *testing.T, h *harness, mode string) (sid, ctx string) {
	t.Helper()
	path := os.Getenv("VUTTY_LIVE_KUBECONFIG")
	if path == "" {
		t.Skip("VUTTY_LIVE_KUBECONFIG is not set")
	}
	ctx = os.Getenv("VUTTY_LIVE_CONTEXT")
	if mode == "kubeconfig" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		h.core.Kube = testKube{}
		cl, err := h.app.Cluster.Add(ClusterInput{Label: "live", Kubeconfig: string(data)})
		if err != nil {
			t.Fatal(err)
		}
		info, err := h.app.Cluster.Open(cl.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = h.app.Server.Close(info.SID) })
		return info.SID, ctx
	}
	// a host: what it is asked to run is run here, by the real kubectl
	sid, _ = h.k8sSession(t, func(cmd string) (string, string, int) {
		c := exec.Command("sh", "-c", cmd)
		c.Env = append(os.Environ(), "KUBECONFIG="+path)
		var out, errb bytes.Buffer
		c.Stdout, c.Stderr = &out, &errb
		code := 0
		if err := c.Run(); err != nil {
			code = 1
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		return out.String(), errb.String(), code
	})
	return sid, ctx
}

func TestLiveCluster(t *testing.T) {
	for _, mode := range []string{"kubeconfig", "host"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t).unlocked(t)
			sid, ctx := liveSession(t, h, mode)
			s := h.app.Server

			all, err := s.K8sResources(sid, ctx)
			if err != nil {
				t.Fatal(err)
			}
			kinds := map[string]K8sAPIResource{}
			for _, r := range all {
				kinds[r.Kind] = r
			}
			for _, want := range []string{"Pod", "Deployment", "Widget", "StorageClass", "Node"} {
				if _, ok := kinds[want]; !ok {
					t.Errorf("discovery has no %s (%d kinds)", want, len(all))
				}
			}
			if _, ok := kinds["Secret"]; ok {
				t.Error("discovery offers Secrets")
			}
			t.Logf("%d kinds", len(all))

			w := kinds["Widget"]
			wl, err := s.K8sObjects(sid, ctx, "shop", K8sResRef{Group: w.Group, Version: w.Version, Plural: w.Plural})
			if err != nil || len(wl.Rows) != 1 || strings.Join(wl.Columns, ",") != "Size,Phase" || strings.Join(wl.Rows[0].Cells, ",") != "large,Ready" {
				t.Fatalf("widgets = %+v, %v", wl, err)
			}
			if y, err := s.K8sObjectYAML(sid, ctx, "shop", K8sResRef{Group: w.Group, Version: w.Version, Plural: w.Plural}, "w1"); err != nil || !strings.Contains(y, "size: large") || strings.Contains(y, "managedFields") {
				t.Fatalf("yaml = %q, %v", y, err)
			}
			sc := kinds["StorageClass"]
			scl, err := s.K8sObjects(sid, ctx, "", K8sResRef{Group: sc.Group, Version: sc.Version, Plural: sc.Plural})
			if err != nil || len(scl.Rows) == 0 || scl.Columns[0] != "Provisioner" || scl.Rows[0].Cells[0] == "" {
				t.Fatalf("storage classes = %+v, %v", scl, err)
			}
			t.Logf("storage class %+v", scl.Rows[0])

			counts, err := s.K8sCounts(sid, ctx, "shop")
			if err != nil || counts["pods"] < 3 || counts["workloads"] != 1 || counts["network"] != 1 {
				t.Fatalf("counts = %v, %v", counts, err)
			}
			t.Logf("counts %v", counts)
			if c, err := s.K8sCounts(sid, ctx, "shop,kube-system"); err != nil || c["pods"] <= counts["pods"] {
				t.Fatalf("counts of two namespaces = %v, %v", c, err)
			}

			ml, err := s.K8sLogsMulti(sid, ctx, "shop", "deployment", "web-demo", K8sLogOpts{Lines: 300, Timestamps: true})
			if err != nil || ml.Pods < 3 || !strings.Contains(ml.Text, "[web-demo-") {
				t.Fatalf("merged logs = %+v, %v", ml, err)
			}
			t.Logf("merged logs: %d pods, %d skipped, failed %v, %d lines", ml.Pods, ml.Skipped, ml.Failed, strings.Count(ml.Text, "\n"))
			if sl, err := s.K8sLogsMulti(sid, ctx, "shop", "service", "web-demo", K8sLogOpts{Lines: 100}); err != nil || sl.Pods < 3 {
				t.Fatalf("service logs = %+v, %v", sl, err)
			}

			rels, err := s.K8sHelmReleases(sid, ctx, "")
			if err != nil || len(rels) != 1 || rels[0].Name != "web" || rels[0].Revision != 2 || rels[0].Status != "deployed" {
				t.Fatalf("releases = %+v, %v", rels, err)
			}
			d, err := s.K8sHelmRelease(sid, ctx, "shop", "web", 0)
			if err != nil {
				t.Fatal(err)
			}
			if d.Chart != "demo" || d.ChartVersion != "0.1.0" || d.AppVersion != "1.16.0" || len(d.History) != 2 || !d.HasValues || d.Release.Revision != 2 {
				t.Fatalf("release = %+v", d)
			}
			if strings.Contains(d.Manifest, "hunter2") || !strings.Contains(d.Manifest, "kind: Deployment") || !strings.Contains(d.Manifest, "<hidden>") {
				t.Fatalf("manifest:\n%s", d.Manifest)
			}
			if v, err := s.K8sHelmValues(sid, ctx, "shop", "web", 1); err != nil || !strings.Contains(v, "hunter2") {
				t.Fatalf("values = %q, %v", v, err)
			}
			t.Logf("release %s %s, notes %d bytes, manifest %d bytes", d.Chart, d.ChartVersion, len(d.Notes), len(d.Manifest))
		})
	}
}

// The files of a pod, by the same two kinds of container the Files section meets: busybox (Alpine) and GNU (Debian), and one with no
// shell. The cluster needs, in the namespace shop, running pods files-alpine and files-debian with a folder /data (notes.txt "hello",
// .env, "report|2026.log", "tên-tệp.txt", empty.txt, big.bin of 3000000 bytes, "dir with space/inner.txt", a folder sub, a folder
// ..hidden_dir, link-to-dir -> sub, link-to-file -> notes.txt, dangling -> /nonexistent) and files-pause (registry.k8s.io/pause:3.9).
func TestLivePodFiles(t *testing.T) {
	for _, pod := range []string{"files-alpine", "files-debian"} {
		t.Run(pod, func(t *testing.T) {
			h := newHarness(t).unlocked(t)
			sid, ctx := liveSession(t, h, "kubeconfig")
			s := h.app.Server
			sh, err := s.K8sShellSession(sid, ctx, "shop", pod, "")
			if err != nil {
				t.Fatal(err)
			}
			if home, err := s.PodFilesHome(sh.HostID); err != nil || home != "/" {
				t.Fatalf("home = %q, %v", home, err)
			}
			list, err := s.PodFilesList(sh.HostID, "/data")
			if err != nil {
				t.Fatal(err)
			}
			by := map[string]string{}
			var order []string
			for _, e := range list {
				kind := "file"
				if e.IsDir {
					kind = "dir"
				}
				by[e.Name] = kind
				order = append(order, e.Name)
			}
			t.Logf("order: %v", order)
			for name, want := range map[string]string{"notes.txt": "file", ".env": "file", "report|2026.log": "file", "tên-tệp.txt": "file", "empty.txt": "file", "big.bin": "file",
				"dir with space": "dir", "sub": "dir", "..hidden_dir": "dir", "link-to-dir": "dir", "link-to-file": "file"} {
				if by[name] != want {
					t.Errorf("%q is %q, want %q (listing: %v)", name, by[name], want, order)
				}
			}
			if _, ok := by["dangling"]; ok {
				t.Log("a dangling link is listed")
			}
			for _, e := range list {
				if e.Name == "big.bin" && (e.Size != 3000000 || e.ModTime.IsZero() || !strings.HasPrefix(e.Mode, "-rw")) {
					t.Errorf("big.bin = %+v", e)
				}
			}
			if inner, err := s.PodFilesList(sh.HostID, "/data/dir with space"); err != nil || len(inner) != 1 || inner[0].Name != "inner.txt" {
				t.Fatalf("a folder with a space: %+v, %v", inner, err)
			}
			if _, err := s.PodFilesList(sh.HostID, "/data/nope"); err == nil || !strings.Contains(err.Error(), "cannot open") {
				t.Fatalf("a folder that is not there: %v", err)
			}
			if empty, err := s.PodFilesList(sh.HostID, "/data/sub"); err != nil || len(empty) != 0 {
				t.Fatalf("an empty folder: %+v, %v", empty, err)
			}
			if root, err := s.PodFilesList(sh.HostID, "/"); err != nil || len(root) < 5 {
				t.Fatalf("the root: %d entries, %v", len(root), err)
			}

			dir := t.TempDir()
			got, err := s.PodFilesFetch(sh.HostID, "/data/notes.txt", dir)
			if b, _ := os.ReadFile(got); err != nil || string(b) != "hello\n" {
				t.Fatalf("notes: %q, %q, %v", got, b, err)
			}
			if got, err := s.PodFilesFetch(sh.HostID, "/data/tên-tệp.txt", dir); err != nil {
				t.Fatalf("a name with accents: %v", err)
			} else if b, _ := os.ReadFile(got); string(b) != "über ünïcode\n" {
				t.Fatalf("content %q", b)
			}
			if got, err := s.PodFilesFetch(sh.HostID, "/data/report|2026.log", dir); err != nil {
				t.Fatalf("a name with a bar: %v", err)
			} else if b, _ := os.ReadFile(got); string(b) != "a|b" {
				t.Fatalf("content %q", b)
			}
			if got, err := s.PodFilesFetch(sh.HostID, "/data/big.bin", dir); err != nil {
				t.Fatalf("a file of 3 MB: %v", err)
			} else if st, _ := os.Stat(got); st.Size() != 3000000 {
				t.Fatalf("size %d", st.Size())
			}
			if got, err := s.PodFilesFetch(sh.HostID, "/data/empty.txt", dir); err != nil {
				t.Fatalf("an empty file: %v", err)
			} else if st, _ := os.Stat(got); st.Size() != 0 {
				t.Fatalf("size %d", st.Size())
			}
			if got, err := s.PodFilesFetch(sh.HostID, "/data/link-to-file", dir); err != nil {
				t.Fatalf("a link to a file: %v", err)
			} else if b, _ := os.ReadFile(got); string(b) != "hello\n" {
				t.Fatalf("content %q", b)
			}
			for path, want := range map[string]string{"/data/sub": "folder", "/data/missing": "", "/dev/null": "regular file"} {
				if _, err := s.PodFilesFetch(sh.HostID, path, dir); err == nil || !strings.Contains(err.Error(), want) {
					t.Errorf("%s: %v (want %q)", path, err, want)
				}
			}
		})
	}
	t.Run("files-pause", func(t *testing.T) {
		h := newHarness(t).unlocked(t)
		sid, ctx := liveSession(t, h, "kubeconfig")
		sh, err := h.app.Server.K8sShellSession(sid, ctx, "shop", "files-pause", "")
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.app.Server.PodFilesList(sh.HostID, "/")
		t.Logf("a container with no shell: %v", err)
		if err == nil || !strings.Contains(err.Error(), "has no shell") {
			t.Fatalf("the answer must say so: %v", err)
		}
	})
}
