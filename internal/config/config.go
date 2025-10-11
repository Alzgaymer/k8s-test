package config

type AppConfig struct {
	Environment Environment
}

type Environment string

func New() AppConfig {
	return AppConfig{
		Environment: "development",
	}
}
