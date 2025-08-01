package http

import (
	"sync"
	"time"

	"CopiRinhaGo/pkg/models"
	"CopiRinhaGo/pkg/handler"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/shopspring/decimal"
)

type Router struct {
	App     *fiber.App
	Handler *handler.PaymentHandler
}

func NewRouter(paymentHandler *handler.PaymentHandler) *Router {
	app := fiber.New(fiber.Config{
		DisableHeaderNormalizing: true,
		JSONEncoder:              json.Marshal,
		JSONDecoder:              json.Unmarshal,
		Prefork:                  false,
		DisableKeepalive:         false,
		ReadTimeout:              800 * time.Millisecond,  // More reasonable timeout
		WriteTimeout:             800 * time.Millisecond,  // More reasonable timeout
		IdleTimeout:              10 * time.Second,        // Reasonable idle timeout
		ReadBufferSize:          131072,  // 128KB buffer
		WriteBufferSize:         131072,  // 128KB buffer
		BodyLimit:               1024,
		Concurrency:             65536,   // Increased concurrency
		DisableStartupMessage:    true,
		ReduceMemoryUsage:        true,   // Enable memory optimization
	})

	return &Router{
		App:     app,
		Handler: paymentHandler,
	}
}

// Object pool for payment requests
var paymentPool = sync.Pool{
	New: func() any {
		return &models.PaymentRequest{}
	},
}

func getPaymentFromPool() *models.PaymentRequest {
	return paymentPool.Get().(*models.PaymentRequest)
}

func putPaymentToPool(payment *models.PaymentRequest) {
	// Reset the payment object before returning to pool
	payment.CorrelationID = ""
	payment.Amount = decimal.Zero
	payment.RequestedAt = time.Time{}
	paymentPool.Put(payment)
}

func (r *Router) RegisterRoutes() {
	// Health check route
	r.App.Get("/health", r.HealthCheck)

	// Payment request route
	r.App.Post("/payments", r.PaymentRequest)

	// Payments summary route
	r.App.Get("/payments-summary", r.PaymentsSummary)
}

func (r *Router) HealthCheck(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{
		"status":  "ok",
		"message": "Service is running",
	})
}

func (r *Router) PaymentRequest(c *fiber.Ctx) error {
	payment := getPaymentFromPool()
	defer putPaymentToPool(payment)

	if err := c.BodyParser(payment); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	payment.RequestedAt = time.Now()

	if err := r.Handler.HandlePaymentRequest(c.Context(), *payment); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to process payment",
		})
	}

	return c.Status(fiber.StatusAccepted).Send(nil)
}

func (r *Router) PaymentsSummary(c *fiber.Ctx) error {
	summary, err := r.Handler.HandlePaymentsSummary(time.Time{}, time.Now())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get summary",
		})
	}

	return c.JSON(summary)
}
