package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/google/go-github/v66/github"

	"github.com/GerardSmit/multirunner/internal/config"
)

func TestClientNextClientReturnsSelf(t *testing.T) {
	c := &Client{scope: config.ScopeRepo, owner: "o", repo: "r"}
	if got := c.NextClient(); got != c {
		t.Error("NextClient should return the same *Client")
	}
}

func TestClientScope(t *testing.T) {
	c := &Client{scope: config.ScopeOrg}
	if c.Scope() != config.ScopeOrg {
		t.Errorf("Scope = %q, want org", c.Scope())
	}
}

func TestRepoSetRoundRobin(t *testing.T) {
	clients := make([]*Client, 3)
	repos := make([]string, 3)
	for i := range clients {
		repos[i] = "repo" + string(rune('A'+i))
		clients[i] = &Client{scope: config.ScopeRepo, owner: "o", repo: repos[i]}
	}
	rs := NewRepoSet(clients, repos)

	if rs.Scope() != config.ScopeRepos {
		t.Errorf("Scope = %q, want repos", rs.Scope())
	}
	if rs.Len() != 3 {
		t.Errorf("Len = %d, want 3", rs.Len())
	}

	// First 6 calls should cycle through repos twice.
	for cycle := 0; cycle < 2; cycle++ {
		for i := 0; i < 3; i++ {
			got := rs.NextClient()
			if got != clients[i] {
				t.Errorf("cycle %d, index %d: got client for %q, want %q",
					cycle, i, got.repo, clients[i].repo)
			}
		}
	}
}

func TestRepoSetRoundRobinConcurrent(t *testing.T) {
	clients := make([]*Client, 4)
	repos := make([]string, 4)
	for i := range clients {
		repos[i] = "repo" + string(rune('0'+i))
		clients[i] = &Client{scope: config.ScopeRepo, owner: "o", repo: repos[i]}
	}
	rs := NewRepoSet(clients, repos)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			c := rs.NextClient()
			if c == nil {
				t.Error("NextClient returned nil")
			}
		}()
	}
	wg.Wait()
}

func TestRepoSetQueuedJobLabels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/repoA/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{{"id": 1}},
			})
		case "/repos/o/repoA/actions/runs/1/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []map[string]any{
					{"status": "queued", "labels": []string{"self-hosted", "linux"}},
				},
			})
		case "/repos/o/repoB/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"workflow_runs": []map[string]any{{"id": 2}},
			})
		case "/repos/o/repoB/actions/runs/2/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []map[string]any{
					{"status": "queued", "labels": []string{"self-hosted", "windows"}},
					{"status": "completed", "labels": []string{"self-hosted", "linux"}},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL + "/")
	makeClient := func(repo string) *Client {
		ghc := github.NewClient(nil)
		ghc.BaseURL = base
		return &Client{gh: ghc, scope: config.ScopeRepo, owner: "o", repo: repo}
	}

	rs := NewRepoSet(
		[]*Client{makeClient("repoA"), makeClient("repoB")},
		[]string{"o/repoA", "o/repoB"},
	)

	labels, err := rs.QueuedJobLabels(context.Background())
	if err != nil {
		t.Fatalf("QueuedJobLabels: %v", err)
	}
	// repoA has 1 queued job, repoB has 1 queued job (completed is filtered).
	if len(labels) != 2 {
		t.Fatalf("got %d label sets, want 2: %v", len(labels), labels)
	}
}

func TestRepoSetQueuedJobLabelsPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/good/actions/runs":
			_ = json.NewEncoder(w).Encode(map[string]any{"workflow_runs": []map[string]any{}})
		case "/repos/o/bad/actions/runs":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL + "/")
	makeClient := func(repo string) *Client {
		ghc := github.NewClient(nil)
		ghc.BaseURL = base
		return &Client{gh: ghc, scope: config.ScopeRepo, owner: "o", repo: repo}
	}

	rs := NewRepoSet(
		[]*Client{makeClient("bad"), makeClient("good")},
		[]string{"o/bad", "o/good"},
	)

	// Should not error even though "bad" repo fails.
	labels, err := rs.QueuedJobLabels(context.Background())
	if err != nil {
		t.Fatalf("QueuedJobLabels: %v", err)
	}
	if len(labels) != 0 {
		t.Errorf("got %d labels from partial failure, want 0", len(labels))
	}
}
