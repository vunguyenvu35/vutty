package usecase

import (
	"strings"
	"testing"
)

func TestCronScheduleAndCommandChecks(t *testing.T) {
	for _, ok := range []string{"* * * * *", "*/5 0-6 1,15 * mon-fri", "@reboot", "  0   3 * * 0 ", "@daily"} {
		if err := validCronSchedule(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "* * * *", "* * * * * *", "@sometimes", "* * * * $(id)", "a;b * * * *", "@daily x"} {
		if validCronSchedule(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if validCronCommand("") == nil || validCronCommand("a\nb") == nil || validCronCommand("a\rb") == nil {
		t.Error("an empty or multi line command was accepted")
	}
	if validCronCommand("/usr/bin/backup.sh >> /var/log/b.log 2>&1") != nil {
		t.Error("a normal command was refused")
	}
}

const sampleCrontab = "# m h dom mon dow command\nMAILTO=me@example.com\n0 3 * * * /usr/bin/backup.sh\n\n@reboot  /usr/bin/start.sh   now\n"

func TestCronAddReplaceDeleteKeepTheOtherLines(t *testing.T) {
	added := cronAdd(sampleCrontab, "*/5  * * * *", "  /bin/ping  ")
	if !strings.HasSuffix(added, "\n*/5 * * * * /bin/ping\n") || !strings.HasPrefix(added, sampleCrontab) {
		t.Fatalf("add: %q", added)
	}
	if got := cronAdd("", "@daily", "x"); got != "@daily x\n" {
		t.Fatalf("add to an empty crontab: %q", got)
	}
	if got := cronAdd("0 1 * * * a", "@daily", "x"); got != "0 1 * * * a\n@daily x\n" {
		t.Fatalf("a last line without a newline: %q", got)
	}
	rep, ok := cronReplace(sampleCrontab, "0 3 * * *", "/usr/bin/backup.sh", "30 4 * * *", "/usr/bin/backup.sh --full")
	if !ok || !strings.Contains(rep, "30 4 * * * /usr/bin/backup.sh --full\n") || strings.Contains(rep, "0 3 * * * /usr/bin/backup.sh\n") || !strings.Contains(rep, "MAILTO=me@example.com") || !strings.Contains(rep, "# m h dom mon dow command") {
		t.Fatalf("replace: ok=%v %q", ok, rep)
	}
	// the list shows the command with single spaces: it still finds a line written with several
	del, ok := cronDelete(sampleCrontab, "@reboot", "/usr/bin/start.sh now")
	if !ok || strings.Contains(del, "start.sh") || !strings.Contains(del, "backup.sh") {
		t.Fatalf("delete: ok=%v %q", ok, del)
	}
	if _, ok := cronDelete(sampleCrontab, "0 4 * * *", "/usr/bin/backup.sh"); ok {
		t.Fatal("deleted a job that is not there")
	}
	if _, ok := cronReplace(sampleCrontab, "0 4 * * *", "x", "@daily", "y"); ok {
		t.Fatal("changed a job that is not there")
	}
	if got, _ := cronDelete("@daily x\n", "@daily", "x"); got != "" {
		t.Fatalf("the last job leaves an empty crontab, got %q", got)
	}
	// of two identical jobs, one goes
	two, _ := cronDelete("@daily x\n@daily x\n", "@daily", "x")
	if two != "@daily x\n" {
		t.Fatalf("two identical jobs: %q", two)
	}
}

func TestCronCallsRefuseWhatCannotBeEditedBeforeTouchingTheServer(t *testing.T) {
	s := &ServerService{}
	for name, err := range map[string]error{
		"owner":    s.CronAdd("x", "/etc/crontab", "@daily", "x"),
		"other":    s.CronAdd("x", "www-data", "@daily", "x"),
		"schedule": s.CronAdd("x", "root", "soon", "x"),
		"command":  s.CronUpdate("x", "root", "@daily", "a", "@daily", "a\nb"),
		"delete":   s.CronDelete("x", "/etc/cron.d/foo", "@daily", "x"),
	} {
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseScheduleReadsTheAccountOfThisConnection(t *testing.T) {
	if got := parseSchedule("@@@cron crontab\n@daily x\n@@@me\ndeploy\n"); got.Me != "deploy" || len(got.Cron) != 1 {
		t.Fatalf("got %+v", got)
	}
}
