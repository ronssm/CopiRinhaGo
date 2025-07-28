package main

import (
    "encoding/json"
    "log"
    "os"
    "time"
    "os/signal"
    "syscall"
    "CopiRinhaGo/db"
    "CopiRinhaGo/handlers"
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/recover"
    "github.com/gofiber/fiber/v2/middleware/limiter"
)

func main() {
    if err := db.Init(); err != nil {
        log.Printf("[ERROR] DB error: %v", err)
        os.Exit(1)
    }
    
    app := fiber.New(fiber.Config{
        Prefork:       false,
        CaseSensitive: true,
        StrictRouting: false,
        ServerHeader:  "",
        AppName:       "CopiRinhaGo",
        DisableKeepalive: false,
        ReadTimeout:      1500 * time.Millisecond,
        WriteTimeout:     1500 * time.Millisecond,
        IdleTimeout:      60 * time.Second,
        ReadBufferSize:  32768,
        WriteBufferSize: 32768,
        BodyLimit:       1024,
        Concurrency:     8192,
        JSONEncoder: json.Marshal,
        JSONDecoder: json.Unmarshal,
        ErrorHandler: func(c *fiber.Ctx, err error) error {
            code := fiber.StatusInternalServerError
            if e, ok := err.(*fiber.Error); ok {
                code = e.Code
            }
            return c.Status(code).JSON(fiber.Map{
                "error": "Internal server error",
            })
        },
    })
    
    app.Use(recover.New(recover.Config{
        EnableStackTrace: false,
    }))
    
    app.Use(limiter.New(limiter.Config{
        Max:        600,
        Expiration: 1 * time.Second,
        KeyGenerator: func(c *fiber.Ctx) string {
            return c.IP()
        },
        LimitReached: func(c *fiber.Ctx) error {
            return c.Status(429).JSON(fiber.Map{
                "error": "Rate limit exceeded",
            })
        },
    }))
    
    app.Get("/health", func(c *fiber.Ctx) error {
        return c.JSON(fiber.Map{"status": "OK"})
    })
    app.Post("/payments", handlers.HandlePaymentFiber)
    app.Get("/payments-summary", handlers.HandlePaymentsSummaryFiber)
    
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-c
        log.Println("Gracefully shutting down...")
        _ = app.ShutdownWithTimeout(30 * time.Second)
        _ = db.Close()
    }()
    
    log.Printf("Starting server on :9999")
    if err := app.Listen(":9999"); err != nil {
        log.Printf("Server failed to start: %v", err)
    }
}
