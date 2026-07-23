package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	HTTPPort     int
	Database     Database
	Kafka        Kafka
	TasksBaseURL string
	TasksSource  string
	LogLevel     string
	SyncToken    string
	SyncURL      string
	SyncInterval time.Duration
}

type Database struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
	Driver   string
	Path     string
}

func (d Database) IsSQLite() bool {
	return d.Driver == "sqlite"
}

func (d Database) SQLiteDSN() string {
	return "file:" + strings.ReplaceAll(d.Path, "\\", "/") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

type Kafka struct {
	Brokers []string
	Topic   string
}

func (d Database) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=10",
		d.Host, d.Port, d.User, d.Password, d.Name, d.SSLMode,
	)
}

func (d Database) URL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=10",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

func Load() (Config, error) {
	loadDotEnv(".env")
	if exe, err := os.Executable(); err == nil {
		loadDotEnv(filepath.Join(filepath.Dir(exe), ".env"))
	}

	v := viper.New()
	v.SetEnvPrefix("POMO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("http_port", 8082)
	v.SetDefault("db_host", "localhost")
	v.SetDefault("db_port", 5434)
	v.SetDefault("db_user", "pomodoro")
	v.SetDefault("db_password", "pomodoro")
	v.SetDefault("db_name", "pomodoro")
	v.SetDefault("db_sslmode", "disable")
	v.SetDefault("db_driver", "postgres")
	v.SetDefault("db_path", "pomodoro.db")
	v.SetDefault("sync_token", "")
	v.SetDefault("sync_url", "")
	v.SetDefault("sync_interval_seconds", 60)
	v.SetDefault("kafka_brokers", "localhost:9094")
	v.SetDefault("kafka_topic", "pomodoro.events")
	v.SetDefault("tasks_url", "http://localhost:8081")
	v.SetDefault("tasks_source", "tasks")
	v.SetDefault("log_level", "info")

	cfg := Config{
		HTTPPort: v.GetInt("http_port"),
		Database: Database{
			Host:     v.GetString("db_host"),
			Port:     v.GetInt("db_port"),
			User:     v.GetString("db_user"),
			Password: v.GetString("db_password"),
			Name:     v.GetString("db_name"),
			SSLMode:  v.GetString("db_sslmode"),
			Driver:   v.GetString("db_driver"),
			Path:     v.GetString("db_path"),
		},
		Kafka: Kafka{
			Brokers: strings.Split(v.GetString("kafka_brokers"), ","),
			Topic:   v.GetString("kafka_topic"),
		},
		SyncToken:    v.GetString("sync_token"),
		SyncURL:      strings.TrimRight(v.GetString("sync_url"), "/"),
		SyncInterval: time.Duration(v.GetInt("sync_interval_seconds")) * time.Second,
		TasksBaseURL: strings.TrimRight(v.GetString("tasks_url"), "/"),
		TasksSource:  v.GetString("tasks_source"),
		LogLevel:     v.GetString("log_level"),
	}
	if cfg.HTTPPort < 1 || cfg.HTTPPort > 65535 {
		return Config{}, fmt.Errorf("validate config: invalid http port %d", cfg.HTTPPort)
	}
	return cfg, nil
}

func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.Trim(strings.TrimSpace(line[eq+1:]), `"'`)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}
