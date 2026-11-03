package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture reads what run prints to standard output.
func capture(t *testing.T, args ...string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := run(args)
	w.Close()
	os.Stdout = old
	b := make([]byte, 4096)
	n, _ := r.Read(b)
	return string(b[:n]), err
}

func TestKeygenSignVerifyRoundTripAndNeverOverwritesAKey(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "k")
	out, err := capture(t, "keygen", keyFile)
	if err != nil {
		t.Fatal(err)
	}
	pub := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "public key: ") {
			pub = strings.TrimPrefix(l, "public key: ")
		}
	}
	if pub == "" {
		t.Fatalf("no public key printed: %q", out)
	}
	if st, _ := os.Stat(keyFile); st.Mode().Perm() != 0o600 {
		t.Fatalf("the private key is %v", st.Mode().Perm())
	}
	if _, err := capture(t, "keygen", keyFile); err == nil {
		t.Fatal("a key was overwritten")
	}

	file := filepath.Join(dir, "SHA256SUMS")
	os.WriteFile(file, []byte(strings.Repeat("a", 64)+"  vutty-1.3.0-macos.zip\n"), 0o644)
	if _, err := capture(t, "sign", keyFile, file); err != nil {
		t.Fatal(err)
	}
	if _, err := capture(t, "verify", pub, file); err != nil {
		t.Fatalf("a good signature was refused: %v", err)
	}
	os.WriteFile(file, []byte(strings.Repeat("b", 64)+"  vutty-1.3.0-macos.zip\n"), 0o644) // the file changed after it was signed
	if _, err := capture(t, "verify", pub, file); err == nil {
		t.Fatal("a changed file passed")
	}
	other := filepath.Join(dir, "k2")
	out2, _ := capture(t, "keygen", other)
	os.WriteFile(file, []byte("x\n"), 0o644)
	capture(t, "sign", keyFile, file)
	otherPub := strings.TrimSpace(strings.TrimPrefix(strings.Split(strings.SplitAfter(out2, "public key: ")[1], "\n")[0], ""))
	if _, err := capture(t, "verify", otherPub, file); err == nil {
		t.Fatal("another key's public half passed")
	}
}

func TestSigningKeyFromTheEnvironmentAndBadInput(t *testing.T) {
	dir := t.TempDir()
	keyFile := filepath.Join(dir, "k")
	capture(t, "keygen", keyFile)
	b, _ := os.ReadFile(keyFile)
	file := filepath.Join(dir, "f")
	os.WriteFile(file, []byte("data"), 0o644)
	t.Setenv("UPDATE_SIGNING_KEY", string(b))
	if _, err := capture(t, "sign", "-", file); err != nil {
		t.Fatalf("the key from the environment: %v", err)
	}
	t.Setenv("UPDATE_SIGNING_KEY", "")
	if _, err := capture(t, "sign", "-", file); err == nil {
		t.Fatal("an empty key was used")
	}
	os.WriteFile(keyFile, []byte("not a key\n"), 0o600)
	if _, err := capture(t, "sign", keyFile, file); err == nil {
		t.Fatal("a file that is not a key was used")
	}
	for _, args := range [][]string{{}, {"nope"}, {"keygen"}, {"sign", "a"}, {"verify", "a"}} {
		if _, err := capture(t, args...); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}
