# CopiRinhaGo

Backend for Rinha de Backend 2025

## Recent Updates

- Fixed Go file structure in `handlers/payments.go` (package declaration, removed stray code)
- Resolved Docker build errors: 'expected package, found func' and 'syntax error: non-declaration statement outside function body'
- Payment handler now includes robust health check and fallback logic

## Overview

This project implements a backend for the Rinha de Backend challenge, handling payment requests and providing payment summaries, with robust fallback and health-check logic.

## Architecture

- **Language:** Go
- **Database:** PostgreSQL
- **Load Balancer:** Nginx
- **Containerization:** Docker Compose
- **Endpoints:**
  - `POST /payments`: Intermediates payment requests, chooses the best Payment Processor, handles fallback and records transactions.
  - `GET /payments-summary`: Returns a summary of processed payments by processor, supporting optional `from`/`to` query params.

## Setup

1. Ensure Docker and Docker Compose are installed.
2. Clone this repository.
3. Start the Payment Processors (see challenge instructions).
4. Run:
   ```sh
   docker-compose up --build
   ```
5. Access endpoints via `http://localhost:9999`.

## Challenge Compliance

- Two backend instances behind Nginx (load balanced).
- Resource limits set in `docker-compose.yml`.
- Uses the `payment-processor` network for integration.
- No source code included in submission directory.

## Technologies

- Go
- PostgreSQL
- Nginx
- Docker Compose

## How it Works

- Health-check endpoints are cached to avoid 429 errors.
- Payments are routed to the default processor unless it is failing, then fallback is used.
- All payments are recorded with processor info for accurate summaries.

## Troubleshooting

- If you see Go build errors about 'expected package' or 'syntax error', check that all `.go` files start with a package declaration and contain only valid Go code.

## License

MIT

## Changelog

- 2025-07-24 14:00: Initial scaffold with payment model, handlers, DB logic, Docker Compose, Nginx, and SQL files
- 2025-07-24 15:00: Added health-check caching, fallback logic, and summary endpoint
- 2025-07-24 15:30: Cleaned up docker-compose.yml and nginx.conf for challenge compliance
- 2025-07-24 16:00: Updated README with setup, architecture, and compliance details
- 2025-07-24 16:30: Completed Dockerfile for Go backend
- 2025-07-24 16:45: Removed duplicate code blocks from Go files and configs
- 2025-07-24 17:00: Final review and compliance check for challenge submission
- 2025-07-24 18:00: Fixed Go file structure in `handlers/payments.go`, resolved Docker build errors, improved payment handler logic
