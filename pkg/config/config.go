package config

import (
	"os"
)

type Config struct {
	HTTPServerHost       string
	ProcessorDefaultURL  string
	ProcessorFallbackURL string
	RPCAddr              string
	MaxJobs              int
	JobTimeout           int
	Debug                bool
}

func LoadConfig() Config {
	return Config{
		HTTPServerHost:       getEnv("HTTP_SERVER_HOST", "9999"),
		ProcessorDefaultURL:  getEnv("PROCESSOR_DEFAULT_URL", "http://payment-processor-default:8080"),
		ProcessorFallbackURL: getEnv("PROCESSOR_FALLBACK_URL", "http://payment-processor-fallback:8080"),
		RPCAddr:              getEnv("RPC_ADDR", ":3334"),
		MaxJobs:              100,
		JobTimeout:           30,
		Debug:                getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
