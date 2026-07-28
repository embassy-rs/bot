package github

import (
	"os"

	"github.com/kelseyhightower/envconfig"
	"github.com/sqlbunny/errors"
)

type Config struct {
	AppID int64 `envconfig:"GITHUB_APP_ID" required:"true"`

	// The app's RSA private key, in PEM form. Either inline (as the k8s Secret
	// mounts it) or a path to the .pem file (handier in dev).
	PrivateKey     string `envconfig:"GITHUB_PRIVATE_KEY"`
	PrivateKeyPath string `envconfig:"GITHUB_PRIVATE_KEY_PATH"`

	WebhookSecret string `envconfig:"GITHUB_WEBHOOK_SECRET" required:"true"`
}

func ConfigFromEnv() (*Config, error) {
	c := new(Config)
	err := envconfig.Process("config", c)
	if err != nil {
		return nil, err
	}

	if c.PrivateKey == "" && c.PrivateKeyPath == "" {
		return nil, errors.New("one of CONFIG_GITHUB_PRIVATE_KEY or CONFIG_GITHUB_PRIVATE_KEY_PATH is required")
	}
	if c.PrivateKeyPath != "" {
		key, err := os.ReadFile(c.PrivateKeyPath)
		if err != nil {
			return nil, errors.Errorf("reading CONFIG_GITHUB_PRIVATE_KEY_PATH: %w", err)
		}
		c.PrivateKey = string(key)
	}

	return c, nil
}
