package config

import (
	"errors"
	"fmt"
	"strings"
)

func (c Config) Validate() error {
	var errs []error

	if c.App.ShutdownTimeout <= 0 {
		errs = append(errs, errors.New("app.shutdown_timeout must be greater than zero"))
	}
	if c.Workers.Count <= 0 {
		errs = append(errs, errors.New("workers.count must be greater than zero"))
	}
	if c.Workers.QueueSize <= 0 {
		errs = append(errs, errors.New("workers.queue_size must be greater than zero"))
	}
	if c.Workers.TaskTimeout <= 0 {
		errs = append(errs, errors.New("workers.task_timeout must be greater than zero"))
	}

	seenNames := make(map[string]struct{}, len(c.Listeners))
	for i, listener := range c.Listeners {
		path := fmt.Sprintf("listeners[%d]", i)
		name := strings.TrimSpace(listener.Name)
		if name == "" {
			errs = append(errs, fmt.Errorf("%s.name is required", path))
		} else if _, exists := seenNames[name]; exists {
			errs = append(errs, fmt.Errorf("%s.name %q is duplicated", path, name))
		} else {
			seenNames[name] = struct{}{}
		}
		if listener.Type != ListenerTelegram {
			errs = append(errs, fmt.Errorf("%s.type %q is not supported", path, listener.Type))
		}
		if listener.Enabled && strings.TrimSpace(listener.Token) == "" {
			errs = append(errs, fmt.Errorf("%s token is required when enabled", path))
		}
	}

	if c.Storage.Provider != ProviderMemory && c.Storage.Provider != ProviderPostgres {
		errs = append(errs, fmt.Errorf("storage.provider %q is not supported", c.Storage.Provider))
	}
	if c.Storage.Provider == ProviderPostgres {
		postgres := c.Storage.Postgres
		if strings.TrimSpace(postgres.Host) == "" {
			errs = append(errs, errors.New("storage.postgres.host is required"))
		}
		if postgres.Port == 0 {
			errs = append(errs, errors.New("storage.postgres.port must be greater than zero"))
		}
		if strings.TrimSpace(postgres.Database) == "" {
			errs = append(errs, errors.New("storage.postgres.database is required"))
		}
		if strings.TrimSpace(postgres.User) == "" {
			errs = append(errs, errors.New("storage.postgres.user is required"))
		}
		if postgres.Password == "" {
			errs = append(errs, errors.New("storage.postgres password is required"))
		}
		if strings.TrimSpace(postgres.SSLMode) == "" {
			errs = append(errs, errors.New("storage.postgres.ssl_mode is required"))
		}
		if postgres.MinConnections < 0 {
			errs = append(errs, errors.New("storage.postgres.min_connections must not be negative"))
		}
		if postgres.MaxConnections <= 0 {
			errs = append(errs, errors.New("storage.postgres.max_connections must be greater than zero"))
		}
		if postgres.MinConnections > postgres.MaxConnections {
			errs = append(errs, errors.New("storage.postgres.min_connections must not exceed max_connections"))
		}
		if postgres.ConnectTimeout <= 0 {
			errs = append(errs, errors.New("storage.postgres.connect_timeout must be greater than zero"))
		}
	}
	if c.Speech.Provider != ProviderMock {
		errs = append(errs, fmt.Errorf("speech.provider %q is not supported", c.Speech.Provider))
	}
	if c.Speech.Mock.Delay < 0 {
		errs = append(errs, errors.New("speech.mock.delay must not be negative"))
	}
	if strings.TrimSpace(c.Speech.Mock.Transcript) == "" && c.Speech.Mock.Error == "" {
		errs = append(errs, errors.New("speech.mock.transcript is required when no mock error is configured"))
	}
	if c.LLM.Provider != ProviderMock {
		errs = append(errs, fmt.Errorf("llm.provider %q is not supported", c.LLM.Provider))
	}
	if c.LLM.Mock.SummaryDelay < 0 {
		errs = append(errs, errors.New("llm.mock.summary_delay must not be negative"))
	}
	if c.LLM.Mock.AnswerDelay < 0 {
		errs = append(errs, errors.New("llm.mock.answer_delay must not be negative"))
	}
	if strings.TrimSpace(c.LLM.Mock.Summary) == "" && c.LLM.Mock.SummaryError == "" {
		errs = append(errs, errors.New("llm.mock.summary is required when no mock error is configured"))
	}
	if strings.TrimSpace(c.LLM.Mock.Answer) == "" && c.LLM.Mock.AnswerError == "" {
		errs = append(errs, errors.New("llm.mock.answer is required when no mock error is configured"))
	}

	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("logging.level %q is not supported", c.Logging.Level))
	}
	switch c.Logging.Format {
	case "text", "json":
	default:
		errs = append(errs, fmt.Errorf("logging.format %q is not supported", c.Logging.Format))
	}

	return errors.Join(errs...)
}
