package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "# comment\n" +
		"export MR_TEST_PAT=\"ghp_fromdotenv\"\n" +
		"MR_TEST_PLAIN=plainval\n" +
		"MR_TEST_REAL=fromfile\n" +
		"\n"
	if err := os.WriteFile(envFile, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// A real env var must win over the .env file.
	t.Setenv("MR_TEST_REAL", "fromenv")
	os.Unsetenv("MR_TEST_PAT")
	os.Unsetenv("MR_TEST_PLAIN")
	t.Cleanup(func() { os.Unsetenv("MR_TEST_PAT"); os.Unsetenv("MR_TEST_PLAIN") })

	loadDotEnv(dir)

	if got := os.Getenv("MR_TEST_PAT"); got != "ghp_fromdotenv" {
		t.Errorf("MR_TEST_PAT = %q, want ghp_fromdotenv (quotes + export stripped)", got)
	}
	if got := os.Getenv("MR_TEST_PLAIN"); got != "plainval" {
		t.Errorf("MR_TEST_PLAIN = %q, want plainval", got)
	}
	if got := os.Getenv("MR_TEST_REAL"); got != "fromenv" {
		t.Errorf("MR_TEST_REAL = %q, want fromenv (real env wins)", got)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	cases := []struct {
		in, k, v string
		ok       bool
	}{
		{"FOO=bar", "FOO", "bar", true},
		{"export FOO=bar", "FOO", "bar", true},
		{`FOO="bar baz"`, "FOO", "bar baz", true},
		{"FOO='bar'", "FOO", "bar", true},
		{"  # comment", "", "", false},
		{"", "", "", false},
		{"NOEQUALS", "", "", false},
		{"=noval", "", "", false},
	}
	for _, c := range cases {
		k, v, ok := parseDotEnvLine(c.in)
		if ok != c.ok || k != c.k || v != c.v {
			t.Errorf("parseDotEnvLine(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, k, v, ok, c.k, c.v, c.ok)
		}
	}
}
