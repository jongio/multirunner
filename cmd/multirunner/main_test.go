package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/GerardSmit/multirunner/internal/config"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return string(out)
}

func starvedConfig(provisioning config.Provisioning, size int) *config.Config {
	return &config.Config{
		GitHub: config.GitHub{
			Scope: config.ScopeRepos,
			Owner: "o",
			Repos: []string{"o/a", "o/b", "o/c"},
		},
		Provisioning: provisioning,
		Pools:        []config.Pool{{Name: "linux", OS: "linux", Size: size}},
	}
}

// TestWarnStarvedReposFlagsUndersizedPool covers the misconfiguration that made
// six of eleven repos unrunnable: in pool mode every slot pins to one repo for
// its whole life, so a pool smaller than the repo list strands the remainder.
func TestWarnStarvedReposFlagsUndersizedPool(t *testing.T) {
	out := captureStdout(t, func() {
		warnStarvedRepos(starvedConfig(config.ProvisioningPool, 2))
	})
	if !strings.Contains(out, "WARNING") {
		t.Fatalf("no warning for size=2 repos=3: %q", out)
	}
	for _, want := range []string{"size=2", "repos=3", "1 repo", "autoscale"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning missing %q: %q", want, out)
		}
	}
}

func TestWarnStarvedReposSilentWhenPoolCoversEveryRepo(t *testing.T) {
	for _, size := range []int{3, 4} {
		out := captureStdout(t, func() {
			warnStarvedRepos(starvedConfig(config.ProvisioningPool, size))
		})
		if out != "" {
			t.Errorf("size=%d repos=3 warned: %q", size, out)
		}
	}
}

// TestWarnStarvedReposSilentUnderAutoscale pins the reason the warning is
// conditional: autoscale places each runner on the repo that queued the job, so
// a small pool is a capacity choice rather than a starvation bug.
func TestWarnStarvedReposSilentUnderAutoscale(t *testing.T) {
	for _, p := range []config.Provisioning{config.ProvisioningAutoscale, config.ProvisioningWebhook} {
		out := captureStdout(t, func() {
			warnStarvedRepos(starvedConfig(p, 1))
		})
		if out != "" {
			t.Errorf("provisioning=%s warned: %q", p, out)
		}
	}
}

// TestWarnStarvedReposSilentOutsideReposScope pins that single-repo, org, and
// enterprise scopes are unaffected: their runners are not pinned per repo.
func TestWarnStarvedReposSilentOutsideReposScope(t *testing.T) {
	for _, scope := range []config.Scope{config.ScopeRepo, config.ScopeOrg, config.ScopeEnterprise} {
		cfg := starvedConfig(config.ProvisioningPool, 1)
		cfg.GitHub.Scope = scope
		out := captureStdout(t, func() { warnStarvedRepos(cfg) })
		if out != "" {
			t.Errorf("scope=%s warned: %q", scope, out)
		}
	}
}
