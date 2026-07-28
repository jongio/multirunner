package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

// TestCheckActionsEnabledFlagsDisabledRepos covers the misconfiguration found on
// this owner's config: six of eleven repos had Actions switched off, so they
// could never queue a job, yet the scaler polled them every interval and looked
// exactly like idle repos.
func TestCheckActionsEnabledFlagsDisabledRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Only o/b has Actions on.
		on := strings.Contains(r.URL.Path, "/repos/o/b/")
		if _, err := fmt.Fprintf(w, `{"enabled":%v}`, on); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	cfg := starvedConfig(config.ProvisioningAutoscale, 3)
	cfg.GitHub.URL = srv.URL
	cfg.Auth = config.Auth{PAT: "x"}

	var err error
	out := captureStdout(t, func() { err = checkActionsEnabled(context.Background(), cfg) })
	if err == nil {
		t.Fatal("want error when repos have Actions disabled, got nil")
	}
	for _, want := range []string{"WARNING", "2 of 3", "o/a", "o/c", "Settings > Actions"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q: %q", want, out)
		}
	}
	if strings.Contains(out, "o/b,") || strings.Contains(out, ", o/b") {
		t.Errorf("enabled repo o/b listed as disabled: %q", out)
	}
}

func TestCheckActionsEnabledSilentWhenAllEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := io.WriteString(w, `{"enabled":true}`); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer srv.Close()

	cfg := starvedConfig(config.ProvisioningAutoscale, 3)
	cfg.GitHub.URL = srv.URL
	cfg.Auth = config.Auth{PAT: "x"}

	var err error
	out := captureStdout(t, func() { err = checkActionsEnabled(context.Background(), cfg) })
	if err != nil {
		t.Fatalf("want nil error when all repos enabled, got %v", err)
	}
	if out != "" {
		t.Errorf("warned when all repos enabled: %q", out)
	}
}

// TestCheckActionsEnabledSkipsNonReposScope pins that org and enterprise scopes
// make no API calls: they have no per-repo list to validate.
func TestCheckActionsEnabledSkipsNonReposScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL.Path)
	}))
	defer srv.Close()

	for _, scope := range []config.Scope{config.ScopeRepo, config.ScopeOrg, config.ScopeEnterprise} {
		cfg := starvedConfig(config.ProvisioningAutoscale, 3)
		cfg.GitHub.Scope = scope
		cfg.GitHub.URL = srv.URL
		cfg.Auth = config.Auth{PAT: "x"}

		var err error
		out := captureStdout(t, func() { err = checkActionsEnabled(context.Background(), cfg) })
		if err != nil {
			t.Errorf("scope=%s returned error: %v", scope, err)
		}
		if out != "" {
			t.Errorf("scope=%s produced output: %q", scope, out)
		}
	}
}
