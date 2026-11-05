package kubeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	execTimeout   = 30 * time.Second
	execOutputCap = 1 << 20
)

// wellKnownExec are the login helpers that cloud providers and identity tools publish, matched by the name of the program and not by
// its folder: a kubeconfig that names one of these is the usual case and runs without asking. Anything else asks first.
var wellKnownExec = func() func(string) bool {
	names := map[string]bool{
		"aws": true, "aws-iam-authenticator": true, "aws-vault": true, "gke-gcloud-auth-plugin": true, "gcloud": true,
		"az": true, "kubelogin": true, "kubectl-oidc_login": true, "kubectl": true, "doctl": true, "tsh": true,
		"rancher": true, "teleport": true, "linode-cli": true, "ibmcloud": true, "oci": true,
	}
	return func(command string) bool {
		slashed := strings.ReplaceAll(command, `\`, "/") // a Windows path, whichever system this runs on
		base := strings.ToLower(path.Base(slashed))
		base = strings.TrimSuffix(base, ".exe")
		base = strings.TrimSuffix(base, ".cmd")
		if !names[base] {
			return false
		}
		// The name alone is not enough: /tmp/x/aws or \\server\share\aws.exe is whatever somebody put there. A bare name is looked up in
		// the usual folders; a path must be in a folder that only an administrator writes.
		if !strings.Contains(slashed, "/") {
			return true
		}
		return inSystemFolder(slashed)
	}
}()

var (
	pathOnce sync.Once
	pathVar  string
)

// searchPath is the PATH of the user's login shell plus the usual install folders. An app started from the Dock or the Start menu
// does not inherit the shell's PATH, so `aws` or `gke-gcloud-auth-plugin` would not be found otherwise.
func searchPath() string {
	pathOnce.Do(func() {
		var dirs []string
		if runtime.GOOS != "windows" {
			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if out, err := exec.CommandContext(ctx, sh, "-lc", `printf %s "$PATH"`).Output(); err == nil {
				dirs = append(dirs, filepath.SplitList(string(out))...)
			}
		}
		dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), filepath.Join(home, "google-cloud-sdk", "bin"))
		}
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/snap/bin")
		seen := map[string]bool{}
		var uniq []string
		for _, d := range dirs {
			if d != "" && !seen[d] {
				seen[d] = true
				uniq = append(uniq, d)
			}
		}
		pathVar = strings.Join(uniq, string(os.PathListSeparator))
	})
	return pathVar
}

// lookup finds the program: a path as it is, a bare name in searchPath.
func lookup(command string) (string, error) {
	if strings.ContainsAny(command, `/\`) {
		// a relative path would mean a different file depending on the folder the app was started in, which is not what was approved
		if !filepath.IsAbs(command) && !strings.HasPrefix(command, `\\`) {
			return "", fmt.Errorf("the login program %s is given as a relative path: the kubeconfig needs its full path", command)
		}
		return command, nil
	}
	for _, d := range filepath.SplitList(searchPath()) {
		for _, ext := range execExts() {
			p := filepath.Join(d, command+ext)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && (runtime.GOOS == "windows" || st.Mode()&0o111 != 0) {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s is not installed on this computer (the kubeconfig needs it to sign in)", command)
}

func execExts() []string {
	if runtime.GOOS == "windows" {
		return []string{".exe", ".cmd", ".bat"}
	}
	return []string{""}
}

type limited struct {
	b   bytes.Buffer
	max int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.max - l.b.Len(); room > 0 {
		l.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// runExec runs a login plugin once and returns the token it printed, and when it expires (zero when it does not say).
func runExec(ctx context.Context, cfg ExecConfig) (string, time.Time, error) {
	path, err := lookup(cfg.Command)
	if err != nil {
		return "", time.Time{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, cfg.Args...)
	api := cfg.APIVersion
	if api == "" {
		api = "client.authentication.k8s.io/v1"
	}
	env := append([]string{}, os.Environ()...)
	env = append(env, "PATH="+searchPath(), fmt.Sprintf(`KUBERNETES_EXEC_INFO={"apiVersion":%q,"kind":"ExecCredential","spec":{"interactive":false}}`, api))
	for _, e := range cfg.Env {
		env = append(env, e.Name+"="+e.Value)
	}
	cmd.Env = env
	var out, errOut limited
	out.max, errOut.max = execOutputCap, 4<<10
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errOut.b.String())
		if msg == "" {
			msg = err.Error()
		}
		if ctx.Err() != nil {
			msg = "it took too long to answer"
		}
		return "", time.Time{}, fmt.Errorf("the login program %s failed: %s", filepath.Base(cfg.Command), msg)
	}
	var cred struct {
		Status struct {
			Token string `json:"token"`
			Exp   string `json:"expirationTimestamp"`
			Cert  string `json:"clientCertificateData"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out.b.Bytes(), &cred); err != nil {
		return "", time.Time{}, errors.New("the login program did not print a credential")
	}
	if cred.Status.Token == "" {
		if cred.Status.Cert != "" {
			return "", time.Time{}, errors.New("the login program answered with a certificate, which is not supported: it needs to answer with a token")
		}
		return "", time.Time{}, errors.New("the login program printed no token")
	}
	var exp time.Time
	if cred.Status.Exp != "" {
		if t, err := time.Parse(time.RFC3339, cred.Status.Exp); err == nil {
			exp = t
		}
	}
	return cred.Status.Token, exp, nil
}

// systemFolders are where a login helper installed by a package manager or by the provider's installer is (slashes as /, lower case).
var systemFolders = []string{"/usr/", "/bin/", "/snap/bin/", "/opt/homebrew/", "c:/program files/", "c:/program files (x86)/"}

// inSystemFolder says whether a path (slashes as /) is under one of systemFolders, and does not climb out of it with "..".
func inSystemFolder(p string) bool {
	if strings.Contains(p, "..") {
		return false
	}
	low := strings.ToLower(p)
	for _, f := range systemFolders {
		if strings.HasPrefix(low, f) {
			return true
		}
	}
	return false
}

// safeExecEnv says whether a login helper may be started without asking with this variable set by the kubeconfig. The names a provider's
// helper reads (AWS_PROFILE, AWS_REGION, AAD_*, AZURE_*) are fine; anything that changes which program or library runs, or where its
// configuration is read from (LD_PRELOAD, DYLD_*, PATH, PYTHON*, AWS_CONFIG_FILE, CLOUDSDK_PYTHON…), asks first.
func safeExecEnv(name string) bool {
	n := strings.ToUpper(name)
	for _, bad := range []string{"PATH", "PYTHON", "PRELOAD", "LD_", "DYLD_", "CONFIG", "CREDENTIALS_FILE", "CREDENTIAL_PROCESS", "SHELL", "ENV", "HOME", "OPTIONS", "BROWSER", "EDITOR", "PROXY"} {
		if strings.Contains(n, bad) {
			return false
		}
	}
	for _, ok := range []string{"AWS_", "AAD_", "AZURE_", "ARM_", "GOOGLE_", "CLOUDSDK_CORE_", "KUBELOGIN_"} {
		if strings.HasPrefix(n, ok) {
			return true
		}
	}
	return false
}
