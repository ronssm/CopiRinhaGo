package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"CopiRinhaGo/pkg/config"
	"CopiRinhaGo/pkg/handler"
	"CopiRinhaGo/pkg/http"
	"CopiRinhaGo/pkg/rpc"
)

func main() {
	logger := log.New(os.Stdout, "[MAIN] ", log.LstdFlags)
	cfg := config.LoadConfig()
	
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Println("Shutting down gracefully...")
		os.Exit(0)
	}()

	queueClient := rpc.NewQueueClient("")
	paymentHandler := handler.NewPaymentHandler(queueClient, logger)
	router := http.NewRouter(paymentHandler)
	router.RegisterRoutes()

	logger.Printf("Starting API server on port %s", cfg.HTTPServerHost)
	if err := router.App.Listen(":" + cfg.HTTPServerHost); err != nil {
		logger.Fatalf("Failed to start server: %v", err)
	}
}
