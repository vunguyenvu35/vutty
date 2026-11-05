package usecase

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The Schedule section can add, change and delete the jobs of a user crontab: the login's own, or root's. The files of the system
// (/etc/crontab, /etc/cron.d) are listed and not edited. A job is found again by its schedule and command, so a crontab that changed
// since the list was read is not edited blindly; every other line (comments, variables) stays as it was.

var (
	cronFieldRe  = regexp.MustCompile(`^[0-9A-Za-z*,/\-?]{1,64}$`)
	cronAliasRe  = regexp.MustCompile(`^@(reboot|hourly|daily|weekly|monthly|yearly|annually|midnight)$`)
	cronOwnerSet = map[string]bool{"crontab": true, "root": true}
)

// validCronSchedule accepts five time fields or an @word. The ranges are left to crontab, which refuses a bad file when it is installed.
func validCronSchedule(s string) error {
	f := strings.Fields(s)
	if len(f) == 1 && cronAliasRe.MatchString(f[0]) {
		return nil
	}
	if len(f) != 5 {
		return errors.New("a schedule is five fields (minute hour day month weekday) or one of @reboot, @hourly, @daily, @weekly, @monthly, @yearly")
	}
	for _, x := range f {
		if !cronFieldRe.MatchString(x) {
			return errors.New("that is not a cron schedule")
		}
	}
	return nil
}

// validCronCommand accepts one line of text: a second line would be a second job.
func validCronCommand(c string) error {
	switch {
	case strings.TrimSpace(c) == "":
		return errors.New("write the command to run")
	case strings.ContainsAny(c, "\r\n\x00"):
		return errors.New("a command is one line")
	}
	return nil
}

func cronLine(schedule, command string) string {
	return strings.Join(strings.Fields(schedule), " ") + " " + strings.TrimSpace(command)
}

// cronIndex is the line of a crontab that holds this job, or -1.
func cronIndex(lines []string, schedule, command string) int {
	want := strings.Join(strings.Fields(schedule), " ")
	for i, ln := range lines {
		if c, ok := parseCronLine(ln, false); ok && c.Schedule == want && c.Command == strings.Join(strings.Fields(command), " ") {
			return i
		}
	}
	return -1
}

func splitCrontab(content string) []string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func joinCrontab(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func cronAdd(content, schedule, command string) string {
	return joinCrontab(append(splitCrontab(content), cronLine(schedule, command)))
}

func cronReplace(content, oldSchedule, oldCommand, schedule, command string) (string, bool) {
	lines := splitCrontab(content)
	i := cronIndex(lines, oldSchedule, oldCommand)
	if i < 0 {
		return "", false
	}
	lines[i] = cronLine(schedule, command)
	return joinCrontab(lines), true
}

func cronDelete(content, schedule, command string) (string, bool) {
	lines := splitCrontab(content)
	i := cronIndex(lines, schedule, command)
	if i < 0 {
		return "", false
	}
	return joinCrontab(append(lines[:i:i], lines[i+1:]...)), true
}

// crontabCmd is the crontab command of an owner: the login's own, or root's.
func crontabCmd(owner string) (string, error) {
	switch owner {
	case "crontab":
		return "crontab", nil
	case "root":
		return "$SUDO crontab -u root", nil
	}
	return "", errors.New("that crontab cannot be edited here")
}

func (s *ServerService) readCrontab(sid, owner string) (string, error) {
	cmd, err := crontabCmd(owner)
	if err != nil {
		return "", err
	}
	return s.run(sid, asRoot+cmd+" -l 2>/dev/null || true")
}

// writeCrontab installs the new crontab (crontab checks it and refuses a bad one) after keeping a copy of the old one next to the home folder.
func (s *ServerService) writeCrontab(sid, owner, content string) error {
	cmd, err := crontabCmd(owner)
	if err != nil {
		return err
	}
	x, err := s.get(sid)
	if err != nil {
		return err
	}
	if x.client == nil {
		return errors.New("this is a cluster reached with a kubeconfig: there is no server to run that on")
	}
	bak := `"$HOME/.crontab.vutty-bak"`
	if owner == "root" {
		bak = "/root/.crontab.vutty-bak"
	}
	script := asRoot + fmt.Sprintf(`( umask 077; %[1]s -l 2>/dev/null | $SUDO tee %[2]s >/dev/null ) 2>/dev/null; %[1]s -`, cmd, bak)
	_, err = runScriptStdin(x.client, script, strings.NewReader(content), serverTimeout)
	return err
}

func checkCronJob(owner, schedule, command string) error {
	if !cronOwnerSet[owner] {
		return errors.New("that crontab cannot be edited here")
	}
	if err := validCronSchedule(schedule); err != nil {
		return err
	}
	return validCronCommand(command)
}

// CronAdd adds a job to the crontab of owner ("crontab": the login's own, or "root").
func (s *ServerService) CronAdd(sid, owner, schedule, command string) error {
	if err := checkCronJob(owner, schedule, command); err != nil {
		return err
	}
	cur, err := s.readCrontab(sid, owner)
	if err != nil {
		return err
	}
	err = s.writeCrontab(sid, owner, cronAdd(cur, schedule, command))
	s.audit(sid, "add a cron job to "+owner, err)
	return err
}

// CronUpdate changes the job with the old schedule and command.
func (s *ServerService) CronUpdate(sid, owner, oldSchedule, oldCommand, schedule, command string) error {
	if err := checkCronJob(owner, schedule, command); err != nil {
		return err
	}
	cur, err := s.readCrontab(sid, owner)
	if err != nil {
		return err
	}
	next, ok := cronReplace(cur, oldSchedule, oldCommand, schedule, command)
	if !ok {
		return errors.New("that job is not in the crontab any more: reload the list")
	}
	err = s.writeCrontab(sid, owner, next)
	s.audit(sid, "change a cron job of "+owner, err)
	return err
}

// CronDelete removes the job with this schedule and command.
func (s *ServerService) CronDelete(sid, owner, schedule, command string) error {
	if !cronOwnerSet[owner] {
		return errors.New("that crontab cannot be edited here")
	}
	cur, err := s.readCrontab(sid, owner)
	if err != nil {
		return err
	}
	next, ok := cronDelete(cur, schedule, command)
	if !ok {
		return errors.New("that job is not in the crontab any more: reload the list")
	}
	err = s.writeCrontab(sid, owner, next)
	s.audit(sid, "delete a cron job of "+owner, err)
	return err
}
