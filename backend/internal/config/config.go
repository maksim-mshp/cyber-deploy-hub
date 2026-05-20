package config

import (
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Env         string     `env:"APP_ENV" envDefault:"local"`
	LogLevel    slog.Level `env:"LOG_LEVEL" envDefault:"info"`
	HTTP        HTTPConfig
	NATS        NATSConfig
	Database    DatabaseConfig
	OpenStack   OpenStackConfig
	KI          KIConfig
	ProjectPool ProjectPoolConfig
}

type HTTPConfig struct {
	Addr string `env:"HTTP_ADDR" envDefault:":8080"`
}

type NATSConfig struct {
	URL string `env:"NATS_URL" envDefault:"nats://localhost:4222"`
}

type DatabaseConfig struct {
	URL      string `env:"DATABASE_URL"`
	MaxConns int32  `env:"DATABASE_MAX_CONNS" envDefault:"10"`
}

type OpenStackConfig struct {
	AuthURL            string        `env:"OS_AUTH_URL" envDefault:"https://edu.cyber-infrastructure.ru:5000/v3"`
	Username           string        `env:"OS_USERNAME"`
	Password           string        `env:"OS_PASSWORD"`
	UserDomainName     string        `env:"OS_USER_DOMAIN_NAME" envDefault:"Hackhaton"`
	ProjectID          string        `env:"OS_PROJECT_ID"`
	ProjectName        string        `env:"OS_PROJECT_NAME"`
	ProjectDomainName  string        `env:"OS_PROJECT_DOMAIN_NAME" envDefault:"Hackhaton"`
	Region             string        `env:"OS_REGION_NAME"`
	AllowReauth        bool          `env:"OS_ALLOW_REAUTH" envDefault:"true"`
	InsecureSkipVerify bool          `env:"OS_INSECURE" envDefault:"false"`
	Timeout            time.Duration `env:"OS_TIMEOUT" envDefault:"20s"`
}

func (c OpenStackConfig) Configured() bool {
	hasProject := c.ProjectID != "" || c.ProjectName != ""
	return c.AuthURL != "" && c.Username != "" && c.Password != "" && hasProject
}

type KIConfig struct {
	APIBaseURL string        `env:"KI_API_BASE_URL" envDefault:"https://edu.cyber-infrastructure.ru:8800"`
	ProjectID  string        `env:"KI_PROJECT_ID"`
	AuthToken  string        `env:"KI_AUTH_TOKEN"`
	Timeout    time.Duration `env:"KI_TIMEOUT" envDefault:"10s"`
}

type ProjectPoolConfig struct {
	SeedFile string `env:"PROJECT_POOL_SEED_FILE"`
	SeedJSON string `env:"PROJECT_POOL_SEED_JSON"`
}

func (c KIConfig) Configured() bool {
	return strings.TrimSpace(c.APIBaseURL) != "" && strings.TrimSpace(c.AuthToken) != ""
}

func Load() (Config, error) {
	return load(env.Options{})
}

func load(opts env.Options) (Config, error) {
	cfg, err := env.ParseAsWithOptions[Config](opts)
	if err != nil {
		return Config{}, err
	}

	if strings.TrimSpace(cfg.HTTP.Addr) == "" {
		return Config{}, errors.New("HTTP_ADDR is empty")
	}
	if strings.TrimSpace(cfg.NATS.URL) == "" {
		return Config{}, errors.New("NATS_URL is empty")
	}
	return cfg, nil
}
