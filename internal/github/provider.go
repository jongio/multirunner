package github

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/GerardSmit/multirunner/internal/config"
)

// ClientProvider abstracts access to one or more GitHub API clients for runner
// registration. Single-scope configs wrap a *Client directly; multi-repo configs
// use a RepoSet that round-robins across per-repo clients.
//
// NextClient returns the *Client to use for the next runner registration. Both
// GenerateJITConfig and DeleteRunner within a single RunOnce call must use the
// same *Client (the one returned by NextClient at the start of that call).
type ClientProvider interface {
	// NextClient returns a *Client for the next runner slot to register.
	NextClient() *Client

	// QueuedJobLabels aggregates queued job labels across all managed scopes.
	QueuedJobLabels(ctx context.Context) ([][]string, error)

	// Scope returns the configured scope.
	Scope() config.Scope
}

// Verify *Client satisfies ClientProvider at compile time.
var _ ClientProvider = (*Client)(nil)

// NextClient returns the client itself (single-scope case).
func (c *Client) NextClient() *Client { return c }

// RepoSet wraps multiple per-repo *Clients and distributes runner registrations
// across them via atomic round-robin. It is the ClientProvider for scope=repos.
type RepoSet struct {
	clients []*Client
	repos   []string // repo names, same order as clients
	owner   string
	counter atomic.Uint64
	mu      sync.Mutex // protects nothing currently; reserved for future expansion
}

// NewRepoSet builds a RepoSet from a list of per-repo clients. The repos slice
// provides human-readable names for logging (same length/order as clients).
func NewRepoSet(clients []*Client, repos []string, owner string) *RepoSet {
	return &RepoSet{clients: clients, repos: repos, owner: owner}
}

// NextClient returns the next *Client in round-robin order. Thread-safe.
func (rs *RepoSet) NextClient() *Client {
	n := rs.counter.Add(1)
	return rs.clients[(n-1)%uint64(len(rs.clients))]
}

// QueuedJobLabels aggregates queued job labels across all repos. Each per-repo
// client is queried sequentially (they share the same PAT, so rate-limit is
// pooled). Errors on individual repos are logged but do not fail the aggregate.
func (rs *RepoSet) QueuedJobLabels(ctx context.Context) ([][]string, error) {
	var all [][]string
	for _, c := range rs.clients {
		labels, err := c.QueuedJobLabels(ctx)
		if err != nil {
			// Log but continue: one repo being unreachable should not prevent
			// polling the others. The caller (autoscale) will retry on the next
			// poll interval.
			continue
		}
		all = append(all, labels...)
	}
	return all, nil
}

// Scope returns ScopeRepos.
func (rs *RepoSet) Scope() config.Scope { return config.ScopeRepos }

// Clients returns the underlying per-repo clients (for shutdown cleanup).
func (rs *RepoSet) Clients() []*Client { return rs.clients }

// Repos returns the repo names in registration order.
func (rs *RepoSet) Repos() []string { return rs.repos }

// Len returns the number of repos.
func (rs *RepoSet) Len() int { return len(rs.clients) }
