package handlers

import (
    "github.com/gofiber/fiber/v2"
    "time"
    "CopiRinhaGo/db"
)

func HandlePaymentsSummaryFiber(c *fiber.Ctx) error {
    var fromTime, toTime *time.Time
    
    if fromStr := c.Query("from"); fromStr != "" {
        if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
            fromTime = &parsed
        } else {
            return c.Status(400).JSON(fiber.Map{"error": "invalid 'from' time format, use RFC3339"})
        }
    }
    
    if toStr := c.Query("to"); toStr != "" {
        if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
            toTime = &parsed
        } else {
            return c.Status(400).JSON(fiber.Map{"error": "invalid 'to' time format, use RFC3339"})
        }
    }

    if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
        return c.Status(400).JSON(fiber.Map{"error": "'from' time must be before 'to' time"})
    }

    summary, err := db.GetOverallPaymentsSummaryWithTimeRange(fromTime, toTime)
    if err != nil {
        return c.JSON(map[string]interface{}{
            "default": map[string]interface{}{
                "totalRequests": 0,
                "totalAmount":   0.0,
            },
            "fallback": map[string]interface{}{
                "totalRequests": 0,
                "totalAmount":   0.0,
            },
        })
    }

    return c.JSON(summary)
}
