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
	
	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Println("Shutting down gracefully...")
		os.Exit(0)
	}()

	// For this simplified version, we'll run everything in a single process
	// Create a simple in-memory queue (no RPC for now)
	queueClient := rpc.NewQueueClient("localhost:3334") // This will be ignored in simple mode

	// Create payment handler
	paymentHandler := handler.NewPaymentHandler(queueClient, logger)

	// Create and configure router
	router := http.NewRouter(paymentHandler)
	router.RegisterRoutes()

	// Start HTTP server
	logger.Printf("Starting API server on port %s", cfg.HTTPServerHost)
	if err := router.App.Listen(":" + cfg.HTTPServerHost); err != nil {
		logger.Fatalf("Failed to start server: %v", err)
	}
}
