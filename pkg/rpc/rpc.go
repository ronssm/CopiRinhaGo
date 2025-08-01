package rpc

import (
	"context"
	"CopiRinhaGo/pkg/models"
	"CopiRinhaGo/pkg/cache"
)

// Simple in-memory implementation for now
type QueueClient struct {
	queue cache.Queue
}

func NewQueueClient(addr string) *QueueClient {
	return &QueueClient{
		queue: cache.NewQueue(10000),
	}
}

func (q *QueueClient) Enqueue(ctx context.Context, request *models.PaymentRequest) error {
	return q.queue.Enqueue(ctx, request)
}

func (q *QueueClient) Dequeue(ctx context.Context) (*models.PaymentRequest, error) {
	item, err := q.queue.Dequeue(ctx)
	if err != nil {
		return nil, err
	}
	if payment, ok := item.(*models.PaymentRequest); ok {
		return payment, nil
	}
	return nil, nil
}

func (q *QueueClient) DequeueBatch(ctx context.Context, batchSize int) ([]*models.PaymentRequest, error) {
	items, err := q.queue.DequeueBatch(ctx, batchSize)
	if err != nil {
		return nil, err
	}
	
	requests := make([]*models.PaymentRequest, 0, len(items))
	for _, item := range items {
		if payment, ok := item.(*models.PaymentRequest); ok {
			requests = append(requests, payment)
		}
	}
	return requests, nil
}
