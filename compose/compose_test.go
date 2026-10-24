package compose

import (
	"errors"
	"testing"
)

func TestParseStatus(t *testing.T) {
	for summary, want := range map[string]Status{
		"running(2)":                StatusRunning,
		"running(1), exited(1)":     StatusPartial,
		"exited(3)":                 StatusStopped,
		"created(1), paused(1)":     StatusStopped,
		"":                          StatusInactive,
		"restarting(1), running(2)": StatusPartial,
	} {
		if got := parseStatus(summary); got != want {
			t.Errorf("parseStatus(%q) = %s, want %s", summary, got, want)
		}
	}
}

func TestParseServicesBothFormats(t *testing.T) {
	array := `[{"Name":"a-web-1","Service":"web","State":"running"},{"Name":"a-db-1","Service":"db","State":"exited"}]`
	lines := "{\"Name\":\"a-web-1\",\"Service\":\"web\",\"State\":\"running\"}\n{\"Name\":\"a-db-1\",\"Service\":\"db\",\"State\":\"exited\"}\n"
	for name, input := range map[string]string{"array": array, "lines": lines} {
		services, err := parseServices([]byte(input))
		if err != nil || len(services) != 2 || services[1].Service != "db" || services[1].State != "exited" {
			t.Errorf("%s: got %+v, %v", name, services, err)
		}
	}
	if services, err := parseServices([]byte("  \n")); err != nil || len(services) != 0 || services == nil {
		t.Errorf("empty: got %#v, %v", services, err)
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"web", "my-app_2", "0day"} {
		if err := ValidateName(ok); err != nil {
			t.Errorf("ValidateName(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Web", "-x", "a/b", "..", "a b", "_x"} {
		if err := ValidateName(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("ValidateName(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestParseAction(t *testing.T) {
	if a, err := ParseAction("up"); err != nil || a != ActionUp {
		t.Fatalf("up: %v %v", a, err)
	}
	if _, err := ParseAction("rm -rf"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad action: %v", err)
	}
}

func TestProjectArgs(t *testing.T) {
	p := Project{Name: "app", Dir: "/srv/app", Files: []string{"/srv/app/compose.yaml", "/srv/app/runtime.compose.yaml"}}
	got := p.args()
	want := []string{"--ansi", "never", "--progress", "plain", "--project-name", "app", "--project-directory", "/srv/app",
		"--file", "/srv/app/compose.yaml", "--file", "/srv/app/runtime.compose.yaml"}
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v", got)
		}
	}
}
