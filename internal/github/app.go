package github

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gh "github.com/google/go-github/v88/github"
	"github.com/sqlbunny/errors"
)

const requestTimeout = 30 * time.Second

// App mints GitHub API clients authenticated either as the app itself or as its
// installation on a repo.
type App struct {
	transport *ghinstallation.AppsTransport

	// ghinstallation.Transport caches the installation token and renews it as
	// it nears expiry, so keep one per installation instead of rebuilding it
	// (and re-minting the token) on every call.
	mu      sync.Mutex
	clients map[int64]*gh.Client
}

func New(config *Config) (*App, error) {
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, config.AppID, []byte(config.PrivateKey))
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &App{
		transport: transport,
		clients:   make(map[int64]*gh.Client),
	}, nil
}

// AppClient returns a client authenticated as the app, which can do little
// besides enumerate its own installations and mint tokens for them.
func (a *App) AppClient() (*gh.Client, error) {
	return gh.NewClient(gh.WithHTTPClient(&http.Client{
		Transport: a.transport,
		Timeout:   requestTimeout,
	}))
}

// Client returns a client authenticated as the app's installation, which is
// what has access to the repos' issues, PRs and statuses.
func (a *App) Client(installationID int64) (*gh.Client, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	client, ok := a.clients[installationID]
	if ok {
		return client, nil
	}

	client, err := gh.NewClient(gh.WithHTTPClient(&http.Client{
		Transport: ghinstallation.NewFromAppsTransport(a.transport, installationID),
		Timeout:   requestTimeout,
	}))
	if err != nil {
		return nil, err
	}

	a.clients[installationID] = client
	return client, nil
}

// CheckAuth verifies the app id and private key actually authenticate, so a bad
// or stale credential is a startup failure rather than a mystery 401 the first
// time a webhook arrives. Returns the app's slug.
func (a *App) CheckAuth(ctx context.Context) (string, error) {
	client, err := a.AppClient()
	if err != nil {
		return "", err
	}

	app, _, err := client.Apps.Get(ctx, "")
	if err != nil {
		return "", errors.WithStack(err)
	}
	return app.GetSlug(), nil
}
