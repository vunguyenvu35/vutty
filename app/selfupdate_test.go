package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"vutty/internal/updater"
)

// a fake GitHub release, signed with a key of the test's own
type fakeRelease struct {
	t       *testing.T
	seed    []byte
	pub     ed25519.PublicKey
	tag     string
	zip     []byte
	zipName string
	srv     *httptest.Server
	// what a test can break
	badSig, noSig, noSums, noZip, draft, prerelease bool
	sumsExtra                                       string // a line added to the list after it was signed
	serveZip                                        []byte // other bytes than the signed ones
	zipSize                                         int64  // what the release says the size is (0: the real one)
	zipHits                                         atomic.Int32
}

func newFakeRelease(t *testing.T, tag string) *fakeRelease {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	rand.Read(seed)
	f := &fakeRelease{t: t, seed: seed, pub: ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey), tag: tag, zip: []byte("PK-the-new-application-bytes"),
		zipName: "vutty-" + strings.TrimPrefix(tag, "v") + "-macos.zip"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeRelease) sums() string {
	sum := sha256.Sum256(f.zip)
	return hex.EncodeToString(sum[:]) + "  " + f.zipName + "\n" + strings.Repeat("1", 64) + "  vutty-other.dmg\n"
}

func (f *fakeRelease) serve(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/latest":
		size := f.zipSize
		if size == 0 {
			size = int64(len(f.zip))
		}
		assets := []map[string]any{}
		add := func(name, path string, size int64) {
			assets = append(assets, map[string]any{"name": name, "browser_download_url": f.srv.URL + path, "size": size})
		}
		if !f.noZip {
			add(f.zipName, "/zip", size)
		}
		if !f.noSums {
			add("SHA256SUMS", "/sums", 100)
		}
		if !f.noSig {
			add("SHA256SUMS.sig", "/sig", 100)
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": f.tag, "draft": f.draft, "prerelease": f.prerelease, "assets": assets})
	case "/zip":
		f.zipHits.Add(1)
		if f.serveZip != nil {
			w.Write(f.serveZip)
			return
		}
		w.Write(f.zip)
	case "/sums":
		w.Write([]byte(f.sums() + f.sumsExtra))
	case "/sig":
		sig, _ := updater.Sign(f.seed, []byte(f.sums())) // signs the list as it was, before sumsExtra
		if f.badSig {
			sig = "AAAA" + sig[4:]
		}
		w.Write([]byte(sig))
	default:
		http.NotFound(w, r)
	}
}

type run struct {
	in       *installer
	applied  []string
	progress []UpdateProgress
}

func (f *fakeRelease) installer(t *testing.T, current string) *run {
	t.Helper()
	r := &run{}
	r.in = &installer{
		client:  &updater.Client{Insecure: true, Allow: func(h string) bool { return h == "127.0.0.1" }},
		apiURL:  f.srv.URL + "/latest",
		current: current,
		pub:     f.pub,
		workDir: filepath.Join(t.TempDir(), "w"),
		extract: func(zipPath, dir string) (string, error) {
			b, err := os.ReadFile(zipPath)
			if err != nil {
				return "", err
			}
			app := filepath.Join(dir, "vutty.app")
			os.MkdirAll(app, 0o755)
			return app, os.WriteFile(filepath.Join(app, "content"), b, 0o644)
		},
		apply:    func(newApp string) error { r.applied = append(r.applied, newApp); return nil },
		progress: func(p UpdateProgress) { r.progress = append(r.progress, p) },
	}
	return r
}

func TestAnUpdateThatIsSignedAndWholeIsUnpackedAndHandedOver(t *testing.T) {
	f := newFakeRelease(t, "v1.3.0")
	r := f.installer(t, "1.2.1")
	if err := r.in.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.applied) != 1 {
		t.Fatalf("applied %v", r.applied)
	}
	if b, _ := os.ReadFile(filepath.Join(r.applied[0], "content")); string(b) != string(f.zip) {
		t.Fatalf("what was unpacked is not what was signed: %q", b)
	}
	var stages []string
	for _, p := range r.progress {
		if len(stages) == 0 || stages[len(stages)-1] != p.Stage {
			stages = append(stages, p.Stage)
		}
	}
	if strings.Join(stages, ",") != "checking,downloading,verifying,installing,restarting" {
		t.Fatalf("stages %v", stages)
	}
	last := r.progress[0]
	for _, p := range r.progress {
		if p.Stage == "downloading" && p.Done > 0 {
			last = p
		}
	}
	if last.Done != int64(len(f.zip)) || last.Total != int64(len(f.zip)) {
		t.Fatalf("download progress %+v", last)
	}
}

func TestNothingIsInstalledWhenSomethingIsWrongWithTheRelease(t *testing.T) {
	type tc struct {
		name    string
		tweak   func(*fakeRelease)
		current string
		want    string
	}
	for _, c := range []tc{
		{"a signature that does not match", func(f *fakeRelease) { f.badSig = true }, "1.2.1", "signature of this release is not valid"},
		{"no signature file", func(f *fakeRelease) { f.noSig = true }, "1.2.1", "published without what a self-update needs"},
		{"no checksum list", func(f *fakeRelease) { f.noSums = true }, "1.2.1", "published without what a self-update needs"},
		{"no application file", func(f *fakeRelease) { f.noZip = true }, "1.2.1", "published without what a self-update needs"},
		{"a line added to the signed list", func(f *fakeRelease) { f.sumsExtra = strings.Repeat("2", 64) + "  vutty-evil.zip\n" }, "1.2.1", "signature of this release is not valid"},
		{"a file that is not the one that was signed", func(f *fakeRelease) { f.serveZip = []byte("PK-something-else") }, "1.2.1", "does not match the signed checksum"},
		{"the same version", func(f *fakeRelease) {}, "1.3.0", "already on the latest"},
		{"an older release than the one running", func(f *fakeRelease) {}, "1.4.0", "already on the latest"},
		{"a draft", func(f *fakeRelease) { f.draft = true }, "1.2.1", "not a final one"},
		{"a prerelease", func(f *fakeRelease) { f.prerelease = true }, "1.2.1", "not a final one"},
		{"a size over the limit", func(f *fakeRelease) { f.zipSize = updater.MaxAppFile + 1 }, "1.2.1", "unexpected size"},
		{"a build that is not a release", func(f *fakeRelease) {}, "dev", "already on the latest"},
	} {
		f := newFakeRelease(t, "v1.3.0")
		c.tweak(f)
		r := f.installer(t, c.current)
		err := r.in.run(context.Background())
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.want)
		}
		if len(r.applied) != 0 {
			t.Errorf("%s: the running application was replaced", c.name)
		}
	}
}

func TestAReleaseSignedByAnotherKeyIsRefusedAndTheAppIsNotEvenDownloaded(t *testing.T) {
	f := newFakeRelease(t, "v1.3.0")
	r := f.installer(t, "1.2.1")
	_, theirs, _ := ed25519.GenerateKey(rand.Reader)
	r.in.pub = theirs.Public().(ed25519.PublicKey) // the app was built with another key than the one that signed the release
	err := r.in.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "signature of this release is not valid") {
		t.Fatalf("%v", err)
	}
	if f.zipHits.Load() != 0 {
		t.Fatal("the application was downloaded before its list was checked")
	}
}

func TestTheNameInTheSignedListMustCarryTheVersionOfTheRelease(t *testing.T) {
	f := newFakeRelease(t, "v1.3.0")
	f.zipName = "vutty-1.2.9-macos.zip" // GitHub's tag says 1.3.0, but the file is another version: a replayed older one
	r := f.installer(t, "1.2.1")
	err := r.in.run(context.Background())
	if err == nil {
		t.Fatal("an older application under a newer tag was installed")
	}
	if len(r.applied) != 0 {
		t.Fatal("replaced")
	}
}

func TestAReleaseThatSendsTheDownloadToAnotherHostIsStopped(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("evil")) }))
	defer evil.Close()
	f := newFakeRelease(t, "v1.3.0")
	f.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/zip" {
			http.Redirect(w, r, strings.Replace(evil.URL, "127.0.0.1", "localhost", 1)+"/x", http.StatusFound)
			return
		}
		f.serve(w, r)
	})
	r := f.installer(t, "1.2.1")
	err := r.in.run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("%v", err)
	}
	if len(r.applied) != 0 {
		t.Fatal("replaced")
	}
}

func TestACancelledUpdateStopsAndLeavesNothingToApply(t *testing.T) {
	f := newFakeRelease(t, "v1.3.0")
	r := f.installer(t, "1.2.1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.in.run(ctx); err == nil {
		t.Fatal("a cancelled update went on")
	}
	if len(r.applied) != 0 {
		t.Fatal("replaced")
	}
}

func TestAFailureToUnpackOrToApplyIsReported(t *testing.T) {
	f := newFakeRelease(t, "v1.3.0")
	r := f.installer(t, "1.2.1")
	r.in.extract = func(string, string) (string, error) { return "", fmt.Errorf("could not unpack the update") }
	if err := r.in.run(context.Background()); err == nil || !strings.Contains(err.Error(), "unpack") {
		t.Fatalf("%v", err)
	}
	r = f.installer(t, "1.2.1")
	r.in.apply = func(string) error { return fmt.Errorf("cannot write there") }
	if err := r.in.run(context.Background()); err == nil || !strings.Contains(err.Error(), "cannot write") {
		t.Fatalf("%v", err)
	}
	for _, p := range r.progress {
		if p.Stage == "restarting" {
			t.Fatal("said it was restarting although the update failed")
		}
	}
}

func TestTheBuiltInKeyIsAValidOneAndOtherSystemsSayTheyCannotUpdateThemselves(t *testing.T) {
	st := NewSystemService().SelfUpdateStatus()
	if st.Reason == "this build has no update key" {
		t.Fatal("the key built into the app is not valid")
	}
	if !st.Supported && st.Reason == "" {
		t.Fatal("a refusal must say why")
	}
}
