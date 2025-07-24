# CopiRinhaGo

Backend for Rinha de Backend 2025

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

## License

MIT

## Changelog

- 2025-07-24 14:00: Initial scaffold with payment model, handlers, DB logic, Docker Compose, Nginx, and SQL files
- 2025-07-24 15:00: Added health-check caching, fallback logic, and summary endpoint
- 2025-07-24 15:30: Cleaned up docker-compose.yml and nginx.conf for challenge compliance
- 2025-07-24 16:00: Updated README with setup, architecture, and compliance details
