package rpc

import (
	"context"
	"CopiRinhaGo/pkg/models"
)

type QueueClient struct{}

func NewQueueClient(addr string) *QueueClient {
	return &QueueClient{}
}

func (q *QueueClient) Enqueue(ctx context.Context, request *models.PaymentRequest) error {
	return nil
}

func (q *QueueClient) Dequeue(ctx context.Context) (*models.PaymentRequest, error) {
	return nil, nil
}
