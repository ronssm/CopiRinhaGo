package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"CopiRinhaGo/pkg/config"
	"CopiRinhaGo/pkg/models"
	"CopiRinhaGo/pkg/rpc"
	"github.com/valyala/fasthttp"
)

type Consumer struct {
	queue             *rpc.QueueClient
	defaultClient     *fasthttp.Client
	fallbackClient    *fasthttp.Client
	defaultURL        string
	fallbackURL       string
	log               *log.Logger
}

func NewConsumer(queue *rpc.QueueClient, cfg config.Config, log *log.Logger) *Consumer {
	// Optimized HTTP clients
	defaultClient := &fasthttp.Client{
		MaxConnsPerHost:         100,
		MaxIdleConnDuration:     30 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		MaxConnWaitTimeout:      5 * time.Second,
	}

	fallbackClient := &fasthttp.Client{
		MaxConnsPerHost:         100,
		MaxIdleConnDuration:     30 * time.Second,
		ReadTimeout:             2 * time.Second,
		WriteTimeout:            2 * time.Second,
		MaxConnWaitTimeout:      5 * time.Second,
	}

	return &Consumer{
		queue:          queue,
		defaultClient:  defaultClient,
		fallbackClient: fallbackClient,
		defaultURL:     cfg.ProcessorDefaultURL,
		fallbackURL:    cfg.ProcessorFallbackURL,
		log:            log,
	}
}

func (c *Consumer) Start(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.processBatch(ctx)
		}
	}
}

func (c *Consumer) processBatch(ctx context.Context) {
	// Try to get a batch of payments
	payments, err := c.queue.DequeueBatch(ctx, 10)
	if err != nil {
		c.log.Printf("Failed to dequeue batch: %v", err)
		return
	}

	if len(payments) == 0 {
		return
	}

	// Process each payment
	for _, payment := range payments {
		if payment != nil {
			go c.processPayment(ctx, payment)
		}
	}
}

func (c *Consumer) processPayment(ctx context.Context, payment *models.PaymentRequest) {
	// Try default processor first
	if c.callProcessor(c.defaultClient, c.defaultURL, payment) {
		c.log.Printf("Payment %s processed by default processor", payment.CorrelationID)
		return
	}

	// Fallback to secondary processor
	if c.callProcessor(c.fallbackClient, c.fallbackURL, payment) {
		c.log.Printf("Payment %s processed by fallback processor", payment.CorrelationID)
		return
	}

	c.log.Printf("Failed to process payment %s", payment.CorrelationID)
}

func (c *Consumer) callProcessor(client *fasthttp.Client, url string, payment *models.PaymentRequest) bool {
	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseRequest(req)
		fasthttp.ReleaseResponse(resp)
	}()

	// Prepare request
	payloadData := map[string]interface{}{
		"correlationId": payment.CorrelationID,
		"amount":        payment.Amount,
		"requestedAt":   payment.RequestedAt.Format(time.RFC3339),
	}

	payload, err := json.Marshal(payloadData)
	if err != nil {
		c.log.Printf("Failed to marshal payment: %v", err)
		return false
	}

	req.Header.SetMethod(http.MethodPost)
	req.Header.SetContentType("application/json")
	req.SetRequestURI(url + "/payments")
	req.SetBody(payload)

	// Make request
	err = client.DoTimeout(req, resp, 2*time.Second)
	if err != nil {
		c.log.Printf("Request failed: %v", err)
		return false
	}

	// Check response
	statusCode := resp.StatusCode()
	return statusCode >= 200 && statusCode < 300
}

func main() {
	logger := log.New(os.Stdout, "[CONSUMER] ", log.LstdFlags)
	cfg := config.LoadConfig()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		logger.Println("Shutting down gracefully...")
		cancel()
	}()

	// Create RPC client for queue operations
	queueClient := rpc.NewQueueClient(cfg.RPCAddr)

	// Create consumer
	consumer := NewConsumer(queueClient, cfg, logger)

	logger.Println("Starting consumer service")
	consumer.Start(ctx)
}
