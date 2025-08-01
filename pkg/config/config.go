package config

import (
	"os"
)

type Config struct {
	HTTPServerHost string
}

func LoadConfig() Config {
	return Config{
		HTTPServerHost: getEnv("HTTP_SERVER_HOST", "9999"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
