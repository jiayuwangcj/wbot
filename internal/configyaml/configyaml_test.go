package configyaml

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig creates a 0600 YAML file in a temp dir and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadExpandsEnvVars(t *testing.T) {
	t.Setenv("WBOT_TESTYAML_ACCOUNT", "acc-123")
	t.Setenv("WBOT_TESTYAML_REGION", "hk")
	p := writeConfig(t, `
futu:
  login_account: "${WBOT_TESTYAML_ACCOUNT}"
  login_region: "${WBOT_TESTYAML_REGION}"
  init_on_start: "yes"
  listen: 8080
`)
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FUTU_LOGIN_ACCOUNT": "acc-123",
		"FUTU_LOGIN_REGION":  "hk",
		"FUTU_INIT_ON_START": "yes",
		"FUTU_LISTEN":        "8080",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q; want %q", k, got[k], v)
		}
	}
}

func TestLoadUndefinedVarFails(t *testing.T) {
	p := writeConfig(t, "futu:\n  login_account: \"${WBOT_TESTS_ONLY_UNSET_VAR_20260801}\"\n")
	_, err := Load(p)
	if err == nil {
		t.Fatal("Load succeeded; want error for undefined variable")
	}
	if !strings.Contains(err.Error(), "WBOT_TESTS_ONLY_UNSET_VAR_20260801") {
		t.Fatalf("error %q must name the variable", err)
	}
	if !strings.Contains(err.Error(), "futu.login_account") {
		t.Fatalf("error %q must name the config key", err)
	}
}

func TestLoadEmptyEnvVarFails(t *testing.T) {
	t.Setenv("WBOT_TESTYAML_EMPTY", "")
	p := writeConfig(t, "futu:\n  login_region: \"${WBOT_TESTYAML_EMPTY}\"\n")
	if _, err := Load(p); err == nil {
		t.Fatal("Load succeeded; want error for empty variable without default")
	}
}

func TestLoadDefaultValue(t *testing.T) {
	t.Setenv("WBOT_TESTYAML_REGION", "us")
	p := writeConfig(t, `
futu:
  login_region: "${WBOT_TESTYAML_REGION:-sh}"
  init_on_start: "${WBOT_TESTS_ONLY_UNSET_VAR_20260801:-yes}"
  login_region_empty_default: "${WBOT_TESTS_ONLY_UNSET_VAR_20260801:-}"
`)
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got["FUTU_LOGIN_REGION"] != "us" {
		t.Errorf("FUTU_LOGIN_REGION = %q; want env value us", got["FUTU_LOGIN_REGION"])
	}
	if got["FUTU_INIT_ON_START"] != "yes" {
		t.Errorf("FUTU_INIT_ON_START = %q; want default yes", got["FUTU_INIT_ON_START"])
	}
	if got["FUTU_LOGIN_REGION_EMPTY_DEFAULT"] != "" {
		t.Errorf("FUTU_LOGIN_REGION_EMPTY_DEFAULT = %q; want empty default", got["FUTU_LOGIN_REGION_EMPTY_DEFAULT"])
	}
}

func TestLoadNestedFlattening(t *testing.T) {
	p := writeConfig(t, `
futu:
  open-d:
    host: "127.0.0.1"
    port: 11111
wechat:
  credentials:
    appid: "wx-test"
`)
	got, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"FUTU_OPEN_D_HOST":         "127.0.0.1",
		"FUTU_OPEN_D_PORT":         "11111",
		"WECHAT_CREDENTIALS_APPID": "wx-test",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q; want %q", k, got[k], v)
		}
	}
}

func TestLoadPermissionCheck(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte("a: b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "0600") {
		t.Fatalf("err = %v; want 0600 permission error", err)
	}
}

func TestLoadRejectsListsAndRootScalars(t *testing.T) {
	for _, content := range []string{
		"futu:\n  tags:\n    - a\n    - b\n",
		"just-a-string\n",
	} {
		p := writeConfig(t, content)
		if _, err := Load(p); err == nil {
			t.Fatalf("Load(%q) succeeded; want error", content)
		}
	}
}

func TestLoadInvalidKey(t *testing.T) {
	p := writeConfig(t, "futu:\n  \"bad key\": v\n")
	if _, err := Load(p); err == nil {
		t.Fatal("Load succeeded; want error for key with space")
	}
}

func TestExpand(t *testing.T) {
	t.Setenv("WBOT_TESTYAML_A", "alpha")
	tests := []struct {
		name, in, want string
	}{
		{"literal", "plain", "plain"},
		{"mixed", "pre-${WBOT_TESTYAML_A}-post", "pre-alpha-post"},
		{"default used", "${WBOT_TESTS_ONLY_UNSET_VAR_20260801:-fallback}", "fallback"},
		{"env wins", "${WBOT_TESTYAML_A:-fallback}", "alpha"},
		{"nested default", "${WBOT_TESTS_ONLY_UNSET_VAR_20260801:-${WBOT_TESTYAML_A}}", "alpha"},
		{"multiple refs", "${WBOT_TESTYAML_A}${WBOT_TESTS_ONLY_UNSET_VAR_20260801:-x}", "alphax"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Expand(%q) = %q; want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandErrors(t *testing.T) {
	for _, in := range []string{
		"${WBOT_TESTS_ONLY_UNSET_VAR_20260801}",
		"unterminated ${WBOT_TESTYAML_A",
		"${}",
		"${1BAD}",
	} {
		if _, err := Expand(in); err == nil {
			t.Errorf("Expand(%q) succeeded; want error", in)
		}
	}
}
