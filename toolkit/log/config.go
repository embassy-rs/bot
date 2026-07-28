package log

import "github.com/kelseyhightower/envconfig"

type Config struct {
	LogFormat string `envconfig:"LOG_FORMAT" default:"text"`
}

func ConfigFromEnv() (*Config, error) {
	c := new(Config)
	err := envconfig.Process("config", c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
