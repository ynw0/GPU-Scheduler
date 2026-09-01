package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Controller    Controller    `toml:"controller"`
	Agent         Agent         `toml:"agent"`
	Security      Security      `toml:"security"`
	Retention     Retention     `toml:"retention"`
	Notifications Notifications `toml:"notifications"`
}

type Controller struct {
	ListenAddress         string `toml:"listen_address"`
	PublicBaseURL         string `toml:"public_base_url"`
	DatabasePath          string `toml:"database_path"`
	AgentSocket           string `toml:"agent_socket"`
	WebhookTimeoutSeconds int    `toml:"webhook_timeout_seconds"`
	ReservedCPUIds        []int  `toml:"reserved_cpu_ids"`
}

type Agent struct {
	SocketPath               string   `toml:"socket_path"`
	DockerSocket             string   `toml:"docker_socket"`
	StateDirectory           string   `toml:"state_directory"`
	SampleIntervalSeconds    int      `toml:"sample_interval_seconds"`
	ReconcileIntervalSeconds int      `toml:"reconcile_interval_seconds"`
	AllowedMountRoots        []string `toml:"allowed_mount_roots"`
}

type Security struct {
	MasterKeyPath        string `toml:"master_key_path"`
	SessionIdleHours     int    `toml:"session_idle_hours"`
	SessionAbsoluteHours int    `toml:"session_absolute_hours"`
	PasswordMinLength    int    `toml:"password_min_length"`
}

type Retention struct {
	RawMetricDays    int `toml:"raw_metric_days"`
	RollupMetricDays int `toml:"rollup_metric_days"`
	BackupCount      int `toml:"backup_count"`
}

type Notifications struct {
	SMTP    SMTP    `toml:"smtp"`
	Webhook Webhook `toml:"webhook"`
}

type SMTP struct {
	Enabled    bool   `toml:"enabled"`
	Address    string `toml:"address"`
	ServerName string `toml:"server_name"`
	Username   string `toml:"username"`
	Password   string `toml:"password"`
	From       string `toml:"from"`
}

type Webhook struct {
	Enabled bool   `toml:"enabled"`
	URL     string `toml:"url"`
	Secret  string `toml:"secret"`
}

func Load(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	switch {
	case c.Controller.ListenAddress == "":
		return errors.New("controller.listen_address is required")
	case c.Controller.DatabasePath == "":
		return errors.New("controller.database_path is required")
	case c.Controller.AgentSocket == "":
		return errors.New("controller.agent_socket is required")
	case c.Agent.SocketPath == "":
		return errors.New("agent.socket_path is required")
	case c.Agent.DockerSocket == "":
		return errors.New("agent.docker_socket is required")
	case c.Security.PasswordMinLength < 6:
		return errors.New("security.password_min_length must be at least 6")
	case c.Retention.RawMetricDays <= 0 || c.Retention.RollupMetricDays <= 0:
		return errors.New("retention days must be positive")
	}
	return nil
}

func (c Config) SampleInterval() time.Duration {
	return time.Duration(c.Agent.SampleIntervalSeconds) * time.Second
}

func (c Config) ReconcileInterval() time.Duration {
	return time.Duration(c.Agent.ReconcileIntervalSeconds) * time.Second
}
