package bot

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// Where the review queue page is served from. Used in comment bodies.
	PublicURL string `envconfig:"PUBLIC_URL" default:"http://localhost:3000"`

	ContributorGuideURL string `envconfig:"CONTRIBUTOR_GUIDE_URL" default:"https://github.com/embassy-rs/embassy/blob/main/CONTRIBUTING.md"`

	// How long a PR gets to stop being red, counted from CI going red, before we
	// point out that it is.
	CIGracePeriod time.Duration `envconfig:"CI_GRACE_PERIOD" default:"1h"`

	// How often to look for PRs whose grace period has elapsed.
	TickInterval time.Duration `envconfig:"TICK_INTERVAL" default:"1m"`

	// Set to true to log what would be posted instead of posting it. Handy when
	// pointing a dev instance at a live repo.
	DryRun bool `envconfig:"DRY_RUN" default:"false"`
}

func ConfigFromEnv() (*Config, error) {
	c := new(Config)
	err := envconfig.Process("config", c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
