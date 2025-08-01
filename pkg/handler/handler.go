package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"log"

	"CopiRinhaGo/pkg/models"
	"CopiRinhaGo/pkg/rpc"
	"CopiRinhaGo/pkg/stats"
)

type PaymentHandler struct {
	queue      *rpc.QueueClient
	log        *log.Logger
	httpClient *http.Client
}

func NewPaymentHandler(queue *rpc.QueueClient, log *log.Logger) *PaymentHandler {
	return &PaymentHandler{
		queue: queue,
		log:   log,
		httpClient: &http.Client{
			Timeout: 1200 * time.Millisecond, // More reasonable timeout, below test script's 1500ms
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	}
}

func (h *PaymentHandler) HandlePaymentRequest(ctx context.Context, request models.PaymentRequest) error {
	// Try default processor first (has lower fees)
	processorType := "default"
	h.log.Printf("Attempting to process payment %s via %s processor", request.CorrelationID, processorType)
	
	err := h.sendPaymentToProcessor(ctx, request, "http://payment-processor-default:8080/payments")
	
	if err != nil {
		h.log.Printf("Default processor failed for payment %s: %v", request.CorrelationID, err)
		// Fallback to fallback processor
		processorType = "fallback"
		h.log.Printf("Attempting to process payment %s via %s processor", request.CorrelationID, processorType)
		err = h.sendPaymentToProcessor(ctx, request, "http://payment-processor-fallback:8080/payments")
		
		if err != nil {
			h.log.Printf("Both processors failed for payment %s. Default error: %v, Fallback error: %v", request.CorrelationID, err, err)
			return fmt.Errorf("payment processing failed: %v", err)
		}
	}
	
	// Record the successful payment in stats AFTER we know it succeeded
	stats.RecordPayment(request.CorrelationID, request.Amount, processorType)
	
	h.log.Printf("Successfully processed payment %s for %s via %s processor", 
		request.CorrelationID, request.Amount.String(), processorType)
	
	return nil
}

func (h *PaymentHandler) sendPaymentToProcessor(ctx context.Context, payment models.PaymentRequest, processorURL string) error {
	h.log.Printf("Sending payment %s to URL: %s", payment.CorrelationID, processorURL)
	
	// Create the request payload with the exact format expected by payment processors
	requestPayload := map[string]interface{}{
		"correlationId": payment.CorrelationID,
		"amount":        payment.Amount,
		"requestedAt":   payment.RequestedAt.Format(time.RFC3339),
	}
	
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal payment request: %w", err)
	}
	
	h.log.Printf("Payment payload for %s: %s", payment.CorrelationID, string(payloadBytes))
	
	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, processorURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	// Send the request
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send payment request: %w", err)
	}
	defer resp.Body.Close()
	
	h.log.Printf("Payment processor returned status %d for payment %s", resp.StatusCode, payment.CorrelationID)
	
	// Check if the payment was successful
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		h.log.Printf("Payment processor error response for %s: %s", payment.CorrelationID, string(bodyBytes))
		return fmt.Errorf("payment processor returned status %d", resp.StatusCode)
	}
	
	return nil
}

func (h *PaymentHandler) HandlePaymentsSummary(start, end time.Time) (*models.PaymentsSummaryResponse, error) {
	return stats.GetPaymentsSummary(start, end), nil
}
