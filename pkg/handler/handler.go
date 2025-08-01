package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
			Timeout: 900 * time.Millisecond,
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
	processorType := "default"
	err := h.sendPaymentToProcessor(ctx, request, "http://payment-processor-default:8080/payments")
	
	if err != nil {
		processorType = "fallback"
		err = h.sendPaymentToProcessor(ctx, request, "http://payment-processor-fallback:8080/payments")
		
		if err != nil {
			return fmt.Errorf("payment processing failed: %v", err)
		}
	}
	
	stats.RecordPayment(request.CorrelationID, request.Amount, processorType)
	
	return nil
}

func (h *PaymentHandler) sendPaymentToProcessor(ctx context.Context, payment models.PaymentRequest, processorURL string) error {
	requestPayload := map[string]interface{}{
		"correlationId": payment.CorrelationID,
		"amount":        payment.Amount,
		"requestedAt":   payment.RequestedAt.Format(time.RFC3339),
	}
	
	payloadBytes, err := json.Marshal(requestPayload)
	if err != nil {
		return fmt.Errorf("failed to marshal payment request: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, processorURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send payment request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("payment processor returned status %d", resp.StatusCode)
	}
	
	return nil
}

func (h *PaymentHandler) HandlePaymentsSummary(start, end time.Time) (*models.PaymentsSummaryResponse, error) {
	return stats.GetPaymentsSummary(start, end), nil
}
