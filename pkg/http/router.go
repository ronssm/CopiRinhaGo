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
		ReadTimeout:              1200 * time.Millisecond,
		WriteTimeout:             1200 * time.Millisecond,
		IdleTimeout:              10 * time.Second,
		ReadBufferSize:          131072,
		WriteBufferSize:         131072,
		BodyLimit:               1024,
		Concurrency:             65536,
		DisableStartupMessage:    true,
		ReduceMemoryUsage:        true,
	})

	return &Router{
		App:     app,
		Handler: paymentHandler,
	}
}

var paymentPool = sync.Pool{
	New: func() any {
		return &models.PaymentRequest{}
	},
}

func getPaymentFromPool() *models.PaymentRequest {
	return paymentPool.Get().(*models.PaymentRequest)
}

func putPaymentToPool(payment *models.PaymentRequest) {
	payment.CorrelationID = ""
	payment.Amount = decimal.Zero
	payment.RequestedAt = time.Time{}
	paymentPool.Put(payment)
}

func (r *Router) RegisterRoutes() {
	r.App.Get("/health", r.HealthCheck)
	r.App.Post("/payments", r.PaymentRequest)
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
	fromStr := c.Query("from")
	toStr := c.Query("to")
	
	var start, end time.Time
	var err error
	
	if fromStr != "" {
		start, err = time.Parse(time.RFC3339, fromStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid 'from' timestamp format, expected RFC3339",
			})
		}
	}
	
	if toStr != "" {
		end, err = time.Parse(time.RFC3339, toStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid 'to' timestamp format, expected RFC3339",
			})
		}
	} else {
		end = time.Now()
	}

	summary, err := r.Handler.HandlePaymentsSummary(start, end)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get summary",
		})
	}

	return c.JSON(summary)
}
