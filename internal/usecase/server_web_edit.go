package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// The Web section can also renew and request Let's Encrypt certificates (certbot) and switch an nginx site on or off (the
// sites-available / sites-enabled layout of Debian and Ubuntu). Names, domains and e-mail addresses are checked against a strict
// pattern before they reach a script.

// WebSiteFile is a file of /etc/nginx/sites-available and whether sites-enabled links to it.
type WebSiteFile struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

var (
	certNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	domainRe    = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
	certEmailRe = regexp.MustCompile(`^[A-Za-z0-9._%+-]{1,64}@[A-Za-z0-9.-]{1,190}\.[A-Za-z]{2,63}$`)
)

const certbotTimeout = 3 * time.Minute

// parseSiteFiles reads "on name" / "off name" lines.
func parseSiteFiles(out string) []WebSiteFile {
	files := []WebSiteFile{}
	for _, ln := range strings.Split(out, "\n") {
		st, name, ok := strings.Cut(strings.TrimSpace(ln), " ")
		if ok && (st == "on" || st == "off") && certNameRe.MatchString(name) {
			files = append(files, WebSiteFile{Name: name, Enabled: st == "on"})
		}
	}
	return files
}

// WebSites lists the nginx site files that can be switched (none when the server does not use sites-available).
func (s *ServerService) WebSites(sid string) ([]WebSiteFile, error) {
	out, err := s.run(sid, `[ -d /etc/nginx/sites-available ] && [ -d /etc/nginx/sites-enabled ] || exit 0
for f in /etc/nginx/sites-available/*; do
  [ -f "$f" ] || continue; n=$(basename "$f")
  if [ -e "/etc/nginx/sites-enabled/$n" ]; then echo "on $n"; else echo "off $n"; fi
done`)
	if err != nil {
		return nil, err
	}
	return parseSiteFiles(out), nil
}

// WebSiteSet links or unlinks a site, then tests the configuration; a configuration that fails is put back as it was and the error
// says why. The web server is not reloaded: the screen asks for that.
func (s *ServerService) WebSiteSet(sid, name string, enabled bool) error {
	if !certNameRe.MatchString(name) {
		return errors.New("that is not a site name")
	}
	script := asRoot + fmt.Sprintf(`N=%s; A=/etc/nginx/sites-available/$N; E=/etc/nginx/sites-enabled/$N
[ -f "$A" ] || { echo "there is no site $N" >&2; exit 1; }
`, name)
	if enabled {
		script += `[ -e "$E" ] && { echo "$N is already on" >&2; exit 1; }
$SUDO ln -s "$A" "$E" || exit 1
OUT=$($SUDO nginx -t 2>&1) || { $SUDO rm -f "$E"; echo "$OUT" >&2; exit 1; }`
	} else {
		script += `[ -L "$E" ] || { echo "$N is not on, or is not a link: remove it by hand" >&2; exit 1; }
T=$(readlink "$E"); $SUDO rm "$E" || exit 1
OUT=$($SUDO nginx -t 2>&1) || { $SUDO ln -s "$T" "$E"; echo "$OUT" >&2; exit 1; }`
	}
	_, err := s.run(sid, script)
	what := "switch off the site "
	if enabled {
		what = "switch on the site "
	}
	s.audit(sid, what+name, err)
	return err
}

// certbotScript checks that certbot is there, then runs it.
func certbotScript(args string) string {
	return asRoot + `command -v certbot >/dev/null 2>&1 || { echo "certbot is not installed on this server" >&2; exit 1; }
$SUDO certbot ` + args + ` 2>&1`
}

// CertRenew renews one certificate (certbot renews only one that is within 30 days of its end) and returns what certbot said.
// force renews it now whatever its age: Let's Encrypt allows only a few of those a week.
func (s *ServerService) CertRenew(sid, name string, force bool) (string, error) {
	if !certNameRe.MatchString(name) {
		return "", errors.New("that is not a certificate name")
	}
	args := "renew --non-interactive --cert-name " + name
	if force {
		args += " --force-renewal"
	}
	out, err := s.runTimeout(sid, certbotScript(args), certbotTimeout)
	if err == nil {
		_, _ = s.run(sid, asRoot+`$SUDO systemctl reload nginx 2>/dev/null || $SUDO systemctl reload caddy 2>/dev/null; true`)
	}
	s.audit(sid, "renew the certificate "+name, err)
	return lastLines(out, err), err
}

// CertRenewTest asks Let's Encrypt for a trial renewal of every certificate and changes nothing.
func (s *ServerService) CertRenewTest(sid string) (string, error) {
	out, err := s.runTimeout(sid, certbotScript("renew --dry-run --non-interactive"), certbotTimeout)
	return lastLines(out, err), err
}

// checkCertRequest validates a certificate request: plain domain names (no wildcard: that needs a DNS challenge) and an address.
func checkCertRequest(domains []string, email string) error {
	if len(domains) == 0 || len(domains) > 20 {
		return errors.New("give one to twenty domains")
	}
	for _, d := range domains {
		if len(d) > 253 || !domainRe.MatchString(d) {
			return errors.New(d + " is not a domain name")
		}
	}
	if !certEmailRe.MatchString(email) {
		return errors.New("give the e-mail address Let's Encrypt can reach you on")
	}
	return nil
}

// CertRequest gets a new certificate with the nginx plug-in of certbot, which also sets up HTTPS in the site that serves the domains.
func (s *ServerService) CertRequest(sid string, domains []string, email string) (string, error) {
	if err := checkCertRequest(domains, email); err != nil {
		return "", err
	}
	args := "--nginx --non-interactive --agree-tos --redirect -m " + email
	for _, d := range domains {
		args += " -d " + d
	}
	out, err := s.runTimeout(sid, certbotScript(args), certbotTimeout)
	s.audit(sid, "get a certificate for "+strings.Join(domains, ", "), err)
	return lastLines(out, err), err
}

// lastLines is what certbot printed, or why it failed (its message comes back as the error, which already holds the output).
func lastLines(out string, err error) string {
	if out == "" && err != nil {
		return err.Error()
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return strings.Join(lines, "\n")
}
