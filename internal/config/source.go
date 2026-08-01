package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnvLookup func(key string) (value string, ok bool)

type Source interface {
	Load(ctx context.Context) (Config, error)
}

type YAMLSource struct {
	path      string
	lookupEnv EnvLookup
}

func NewYAMLSource(path string, lookupEnv EnvLookup) *YAMLSource {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	return &YAMLSource{path: path, lookupEnv: lookupEnv}
}

func (s *YAMLSource) Load(ctx context.Context) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", s.path, err)
	}

	cfg := Default()
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %q: %w", s.path, err)
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return Config{}, fmt.Errorf("decode config %q: %w", s.path, err)
	}

	if err := cfg.resolveSecrets(s.lookupEnv); err != nil {
		return Config{}, fmt.Errorf("resolve config secrets: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c *Config) resolveSecrets(lookup EnvLookup) error {
	for i := range c.Listeners {
		listener := &c.Listeners[i]
		if !listener.Enabled {
			continue
		}
		key := strings.TrimSpace(listener.TokenEnv)
		if key == "" {
			return fmt.Errorf("listener %q: token_env is required", listener.Name)
		}
		value, ok := lookup(key)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("listener %q: required environment variable %q is missing or empty", listener.Name, key)
		}
		listener.Token = value
	}
	return nil
}
