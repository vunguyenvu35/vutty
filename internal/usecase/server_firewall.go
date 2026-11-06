package usecase

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// The Ports section can also change the ufw firewall: allow or deny a port, delete a rule, turn it on or off. Everything that reaches
// the script is a number or an address that was parsed first, and the SSH port of this very connection is guarded, so that the panel
// cannot lock itself out.

// FirewallRule is one rule of `ufw status numbered` (the IPv6 twin of a rule is left out: ufw adds and deletes the pair together).
type FirewallRule struct {
	Num    int    `json:"num"`
	To     string `json:"to"`
	Action string `json:"action"` // ALLOW, DENY, REJECT, LIMIT
	From   string `json:"from"`
}

var ruleNumRe = regexp.MustCompile(`^\[\s*\d+\]\s*`)

var ruleRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.+?)\s{2,}(ALLOW|DENY|REJECT|LIMIT)(?: IN| OUT| FWD)?\s+(.+?)\s*$`)

// parseFirewallRules reads the lines of `ufw status numbered`.
func parseFirewallRules(ufw string) []FirewallRule {
	rules := []FirewallRule{}
	for _, ln := range strings.Split(ufw, "\n") {
		m := ruleRe.FindStringSubmatch(strings.TrimRight(ln, "\r"))
		if m == nil || strings.Contains(m[2], "(v6)") {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		rules = append(rules, FirewallRule{Num: n, To: m[2], Action: m[3], From: m[4]})
	}
	return rules
}

// rulePorts lists the ports a "To" column names: "22/tcp", "80,443/tcp", "8000:8100/tcp", "22".
func rulePorts(to string) []int {
	var ports []int
	for _, p := range strings.Split(strings.SplitN(strings.Fields(to + " ")[0], "/", 2)[0], ",") {
		if lo, hi, ok := strings.Cut(p, ":"); ok {
			a, e1 := strconv.Atoi(lo)
			b, e2 := strconv.Atoi(hi)
			if e1 == nil && e2 == nil && b >= a && b-a < 65536 {
				for n := a; n <= b; n++ {
					ports = append(ports, n)
				}
			}
		} else if n, err := strconv.Atoi(p); err == nil {
			ports = append(ports, n)
		}
	}
	return ports
}

// ufwRuleArgs builds the arguments of `ufw allow|deny`, or an error for anything that is not a plain port rule.
func ufwRuleArgs(action string, port int, proto, from string) (string, error) {
	if action != "allow" && action != "deny" {
		return "", errors.New("a rule allows or denies")
	}
	if port < 1 || port > 65535 {
		return "", errors.New("a port is a number from 1 to 65535")
	}
	if proto != "" && proto != "tcp" && proto != "udp" {
		return "", errors.New("the protocol is tcp or udp")
	}
	from = strings.TrimSpace(from)
	if from != "" {
		if _, _, err := net.ParseCIDR(from); err != nil && net.ParseIP(from) == nil {
			return "", errors.New("the source is an IP address or a network such as 10.0.0.0/8")
		}
	}
	p := strconv.Itoa(port)
	if proto != "" {
		p += "/" + proto
	}
	if from == "" {
		return fmt.Sprintf("%s %s", action, p), nil
	}
	return fmt.Sprintf("%s from %s to any port %d%s", action, from, port, map[bool]string{true: " proto " + proto}[proto != ""]), nil
}

// sshPortOf is the server port of this connection (sshd puts it in SSH_CONNECTION), 22 when it cannot tell.
func (s *ServerService) sshPortOf(sid string) int {
	out, err := s.run(sid, `echo "${SSH_CONNECTION##* }"`)
	if n, e := strconv.Atoi(strings.TrimSpace(out)); err == nil && e == nil && n > 0 && n < 65536 {
		return n
	}
	return 22
}

func (s *ServerService) ufw(sid, args string) error {
	_, err := s.run(sid, asRoot+`command -v ufw >/dev/null 2>&1 || { echo "ufw is not installed on this server" >&2; exit 1; }; $SUDO ufw `+args+` 2>&1`)
	return err
}

// FirewallAdd allows or denies a port ("allow" / "deny"; proto "" is both tcp and udp; from "" is anyone). Denying the SSH port of
// this connection is refused unless force.
func (s *ServerService) FirewallAdd(sid, action string, port int, proto, from string, force bool) error {
	args, err := ufwRuleArgs(action, port, proto, from)
	if err != nil {
		return err
	}
	if action == "deny" && !force && port == s.sshPortOf(sid) {
		return errors.New("that is the port of this connection: denying it would lock you out")
	}
	err = s.ufw(sid, args)
	s.audit(sid, fmt.Sprintf("%s port %d in the firewall", action, port), err)
	return err
}

// FirewallDelete deletes the rule at this number, after checking that it is still the rule the user saw (numbers move when another
// rule goes). Deleting an ALLOW of the SSH port of this connection is refused unless force.
func (s *ServerService) FirewallDelete(sid string, num int, to, action, from string, force bool) error {
	out, err := s.run(sid, asRoot+`$SUDO ufw status numbered 2>&1`)
	if err != nil {
		return err
	}
	var rule *FirewallRule
	for _, r := range parseFirewallRules(out) {
		if r.Num == num {
			r := r
			rule = &r
		}
	}
	if rule == nil || rule.To != to || rule.Action != action || rule.From != from {
		return errors.New("that rule is not there any more: reload the list")
	}
	if !force && rule.Action == "ALLOW" {
		ssh := s.sshPortOf(sid)
		for _, p := range rulePorts(rule.To) {
			if p == ssh {
				return errors.New("that rule lets this connection in: deleting it could lock you out")
			}
		}
	}
	err = s.ufw(sid, fmt.Sprintf("--force delete %d", num))
	s.audit(sid, "delete a firewall rule ("+to+" "+action+")", err)
	return err
}

// FirewallEnable turns ufw on, after allowing the SSH port of this connection so the session stays up.
func (s *ServerService) FirewallEnable(sid string) error {
	port := s.sshPortOf(sid)
	if err := s.ufw(sid, fmt.Sprintf("allow %d/tcp", port)); err != nil {
		return err
	}
	err := s.ufw(sid, "--force enable")
	s.audit(sid, "turn the firewall on", err)
	return err
}

// FirewallDisable turns ufw off (the rules stay).
func (s *ServerService) FirewallDisable(sid string) error {
	err := s.ufw(sid, "disable")
	s.audit(sid, "turn the firewall off", err)
	return err
}
