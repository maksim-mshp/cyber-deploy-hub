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
	Capacity    CapacityConfig
	Cloud       CloudConfig
	VDI         VDIConfig
	Lifecycle   LifecycleConfig
	Checker     CheckerConfig
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
	APIBaseURL    string        `env:"KI_API_BASE_URL" envDefault:"https://edu.cyber-infrastructure.ru:8800"`
	ProjectID     string        `env:"KI_PROJECT_ID"`
	AuthToken     string        `env:"KI_AUTH_TOKEN"`
	SessionCookie string        `env:"KI_SESSION_COOKIE"`
	SessionID     string        `env:"KI_SESSION_ID" envDefault:"1"`
	Username      string        `env:"KI_USERNAME"`
	Password      string        `env:"KI_PASSWORD"`
	DomainName    string        `env:"KI_DOMAIN_NAME" envDefault:"Hackhaton"`
	Timeout       time.Duration `env:"KI_TIMEOUT" envDefault:"10s"`
}

type ProjectPoolConfig struct {
	SeedFile string `env:"PROJECT_POOL_SEED_FILE"`
	SeedJSON string `env:"PROJECT_POOL_SEED_JSON"`
}

type CapacityConfig struct {
	ThresholdPercent   float64 `env:"CAPACITY_THRESHOLD_PERCENT" envDefault:"90"`
	DemoVCPUs          int     `env:"CAPACITY_DEMO_VCPUS" envDefault:"128"`
	DemoVCPUsFree      int     `env:"CAPACITY_DEMO_VCPUS_FREE" envDefault:"96"`
	DemoRAMMiB         int64   `env:"CAPACITY_DEMO_RAM_MIB" envDefault:"262144"`
	DemoRAMFreeMiB     int64   `env:"CAPACITY_DEMO_RAM_FREE_MIB" envDefault:"196608"`
	DemoStorageGiB     int64   `env:"CAPACITY_DEMO_STORAGE_GIB" envDefault:"4096"`
	DemoStorageUsedGiB int64   `env:"CAPACITY_DEMO_STORAGE_USED_GIB" envDefault:"1024"`
}

type CloudConfig struct {
	PrivateNetworkID   string        `env:"CLOUD_PRIVATE_NETWORK_ID"`
	PrivateSubnetID    string        `env:"CLOUD_PRIVATE_SUBNET_ID"`
	SecurityGroupIDs   string        `env:"CLOUD_SECURITY_GROUP_IDS"`
	BlueprintFile      string        `env:"CLOUD_BLUEPRINT_FILE"`
	BlueprintJSON      string        `env:"CLOUD_BLUEPRINT_JSON"`
	KeyEncryptionKey   string        `env:"CLOUD_KEY_ENCRYPTION_KEY"`
	KeyEncryptionKeyID string        `env:"CLOUD_KEY_ENCRYPTION_KEY_ID" envDefault:"default"`
	DeployTimeout      time.Duration `env:"CLOUD_DEPLOY_TIMEOUT" envDefault:"20m"`
	PollInterval       time.Duration `env:"CLOUD_POLL_INTERVAL" envDefault:"5s"`
	DeletePollInterval time.Duration `env:"CLOUD_DELETE_POLL_INTERVAL" envDefault:"3s"`
}

type VDIConfig struct {
	HTTPAddr           string        `env:"VDI_HTTP_ADDR" envDefault:":8082"`
	PublicBaseURL      string        `env:"VDI_PUBLIC_BASE_URL" envDefault:"http://localhost:8082"`
	AccessTokenTTL     time.Duration `env:"VDI_ACCESS_TOKEN_TTL" envDefault:"15m"`
	ConsoleURLTemplate string        `env:"VDI_CONSOLE_URL_TEMPLATE" envDefault:"/vdi/console?session={token}"`
}

type LifecycleConfig struct {
	PollInterval             time.Duration `env:"LIFECYCLE_POLL_INTERVAL" envDefault:"5s"`
	DueBatchSize             int           `env:"LIFECYCLE_DUE_BATCH_SIZE" envDefault:"25"`
	DefaultLabTTL            time.Duration `env:"LIFECYCLE_DEFAULT_LAB_TTL" envDefault:"2h"`
	DefaultFreezeTTL         time.Duration `env:"LIFECYCLE_DEFAULT_FREEZE_TTL" envDefault:"24h"`
	DefaultCapacityThreshold float64       `env:"LIFECYCLE_DEFAULT_CAPACITY_THRESHOLD" envDefault:"90"`
}

type CheckerConfig struct {
	ProfileFile           string        `env:"CHECKER_PROFILE_FILE" envDefault:"/app/config/checker_profiles.json"`
	ProfileJSON           string        `env:"CHECKER_PROFILE_JSON"`
	DefaultSSHUser        string        `env:"CHECKER_DEFAULT_SSH_USER" envDefault:"ubuntu"`
	SSHPort               int           `env:"CHECKER_SSH_PORT" envDefault:"22"`
	SSHTimeout            time.Duration `env:"CHECKER_SSH_TIMEOUT" envDefault:"20s"`
	DefaultCommandTimeout time.Duration `env:"CHECKER_COMMAND_TIMEOUT" envDefault:"15s"`
}

func (c KIConfig) Configured() bool {
	if strings.TrimSpace(c.APIBaseURL) == "" {
		return false
	}
	if strings.TrimSpace(c.AuthToken) != "" {
		return true
	}
	hasProject := strings.TrimSpace(c.ProjectID) != ""
	hasSession := strings.TrimSpace(c.SessionCookie) != ""
	hasCredentials := strings.TrimSpace(c.Username) != "" &&
		strings.TrimSpace(c.Password) != "" &&
		strings.TrimSpace(c.DomainName) != ""
	return hasProject && (hasSession || hasCredentials)
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
