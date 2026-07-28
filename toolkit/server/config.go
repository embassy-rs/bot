package server

import (
	"fmt"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	Debug bool           `envconfig:"DEBUG" default:"false"`
	Ports map[string]int `envconfig:"PORTS" required:"true"`
}

func (c *Config) GetPort(name string) int {
	portNum, ok := c.Ports[name]
	if !ok {
		panic(fmt.Sprintf("Port number not found in CONFIG_PORTS for %s", name))
	}
	return portNum
}

func (c *Config) HasPort(name string) bool {
	_, ok := c.Ports[name]
	return ok
}

func ConfigFromEnv() (*Config, error) {
	c := new(Config)
	err := envconfig.Process("config", c)
	if err != nil {
		return nil, err
	}
	return c, nil
}
