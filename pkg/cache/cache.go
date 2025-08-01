package cache

import (
	"context"
	"sync"
)

type Queue interface {
	Enqueue(ctx context.Context, request any) error
	Dequeue(ctx context.Context) (any, error)
	DequeueBatch(ctx context.Context, batchSize int) ([]any, error)
}

type queueImpl struct {
	data []any
	sync.Mutex
}

func NewQueue(size int) Queue {
	return &queueImpl{
		data: make([]any, 0, size),
	}
}

func (q *queueImpl) Enqueue(ctx context.Context, body any) error {
	q.Lock()
	defer q.Unlock()
	q.data = append(q.data, body)
	return nil
}

func (q *queueImpl) Dequeue(ctx context.Context) (any, error) {
	q.Lock()
	defer q.Unlock()
	if len(q.data) == 0 {
		return nil, nil
	}
	body := q.data[0]
	q.data = q.data[1:]
	return body, nil
}

func (q *queueImpl) DequeueBatch(ctx context.Context, batchSize int) ([]any, error) {
	q.Lock()
	defer q.Unlock()
	if len(q.data) == 0 {
		return nil, nil
	}

	if len(q.data) < batchSize {
		batchSize = len(q.data)
	}

	res := q.data[:batchSize]
	q.data = q.data[batchSize:]
	return res, nil
}

type CacheService struct {
	cache []any
	sync.Mutex
}

func NewCacheService(size int) *CacheService {
	return &CacheService{
		cache: make([]any, 0, size),
	}
}

func (cs *CacheService) Add(value any) {
	cs.Lock()
	defer cs.Unlock()
	cs.cache = append(cs.cache, value)
}

func (cs *CacheService) GetList() ([]any, error) {
	cs.Lock()
	defer cs.Unlock()
	copyCache := make([]any, len(cs.cache))
	copy(copyCache, cs.cache)
	return copyCache, nil
}
