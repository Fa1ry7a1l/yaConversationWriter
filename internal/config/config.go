package config

import "time"

const (
	ListenerTelegram = "telegram"
	ProviderMemory   = "memory"
	ProviderMock     = "mock"
)

type Config struct {
	App       App        `yaml:"app"`
	Listeners []Listener `yaml:"listeners"`
	Workers   Workers    `yaml:"workers"`
	Storage   Storage    `yaml:"storage"`
	Speech    Speech     `yaml:"speech"`
	LLM       LLM        `yaml:"llm"`
	Logging   Logging    `yaml:"logging"`
}

type App struct {
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

type Listener struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Enabled  bool   `yaml:"enabled"`
	TokenEnv string `yaml:"token_env"`
	Token    string `yaml:"-"`
}

type Workers struct {
	Count       int           `yaml:"count"`
	QueueSize   int           `yaml:"queue_size"`
	TaskTimeout time.Duration `yaml:"task_timeout"`
}

type Storage struct {
	Provider string `yaml:"provider"`
}

type Speech struct {
	Provider string     `yaml:"provider"`
	Mock     SpeechMock `yaml:"mock"`
}

type SpeechMock struct {
	Transcript string        `yaml:"transcript"`
	Delay      time.Duration `yaml:"delay"`
	Error      string        `yaml:"error"`
}

type LLM struct {
	Provider string  `yaml:"provider"`
	Mock     LLMMock `yaml:"mock"`
}

type LLMMock struct {
	Summary      string        `yaml:"summary"`
	Answer       string        `yaml:"answer"`
	SummaryDelay time.Duration `yaml:"summary_delay"`
	AnswerDelay  time.Duration `yaml:"answer_delay"`
	SummaryError string        `yaml:"summary_error"`
	AnswerError  string        `yaml:"answer_error"`
}

type Logging struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func Default() Config {
	return Config{
		App: App{ShutdownTimeout: 10 * time.Second},
		Workers: Workers{
			Count:       2,
			QueueSize:   32,
			TaskTimeout: 2 * time.Minute,
		},
		Storage: Storage{Provider: ProviderMemory},
		Speech: Speech{
			Provider: ProviderMock,
			Mock:     SpeechMock{Transcript: "Prepared mock transcript"},
		},
		LLM: LLM{
			Provider: ProviderMock,
			Mock: LLMMock{
				Summary: "Prepared mock summary",
				Answer:  "Prepared mock answer",
			},
		},
		Logging: Logging{Level: "info", Format: "text"},
	}
}
