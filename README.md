# Perso — Telegram Marketplace Monitoring & Price Discovery Platform

Perso is an intelligent marketplace monitoring and product-price discovery platform initially designed for the Iranian electronic consumer market (smartphones, laptops, gaming consoles).

It monitors authorized Telegram channels and groups, extracts seller advertisements using deterministic Persian NLP, matches listings against user purchase interests, computes explainable offer scores and price analytics, and manages seller conversation workflows with explicit human-in-the-loop approval.

---

## Key Features

- **MTProto Ingestion (`gotd/td`)**: Monitors authorized Telegram channels/groups without bots or unauthorized scraping. Sessions are encrypted at rest with AES-256-GCM and file-locked against concurrent collector instances.
- **Strict Compliance & Privacy by Design**: Only monitors chat IDs explicitly added to the user's `watch_sources`. External LLM processing is disabled by default (`LLM_ENABLED=false`).
- **Deterministic Persian NLP Engine**: Normalizes Arabic/Persian character variants (ي/ی, ك/ک, ة/ه), Persian and Arabic digits to ASCII, ZWNJ, and punctuation. Extracts Iranian Toman and Rial prices (including shorthand expressions like `۹۸ م ت`, `۹۸ میلیون تومان`, `توافقی`).
- **5-Level Product Matching**: Matches seller listings to canonical catalog products by exact ID, normalized name, Persian/English aliases, brand & model, and multi-keywords, while preserving variant differentiation (e.g. `256GB` vs `128GB`).
- **Explainable Best-Offer Scoring**: Multi-dimensional score (0 to 100) weighting price competitiveness (35%), condition (20%), freshness (15%), confidence (10%), variant match (10%), and tech hub location (10%), with complete textual explanation.
- **Statistical Outlier Rejection**: Interquartile Range (IQR) filtering computes reliable min, max, median, average prices, and 5-bin distribution histograms.
- **Conversation State Machine**: Facilitates collecting missing fields (price, warranty, city, Tehran market hub) with human-in-the-loop review (`AWAITING_APPROVAL`), honest AI identity disclosure, and instant refusal/opt-out honoring.
- **Prompt Injection Defense**: Encapsulates untrusted message contents in XML boundaries, prohibiting prompt alteration and preventing LLM outputs from overwriting verified deterministic prices.
- **Dual-Database Architecture**: MongoDB stores raw Telegram messages, event telemetry, and message histories; PostgreSQL stores catalog, offers, price observations, and user interests.
- **Redis Streams Queue**: Decouples collector ingestion from worker processing with consumer groups and acknowledgments.
- **RTL Web Dashboard**: Interactive responsive Persian RTL web dashboard for monitoring deals, statistics, and conversation workflows.

---

## Technology Stack & Versions

- **Go**: `1.24+` / `1.26` (Idiomatic modular architecture, `log/slog`, `net/http` + `chi/v5`)
- **Telegram MTProto**: `github.com/gotd/td v0.166.0`
- **PostgreSQL**: `16` (`github.com/jackc/pgx/v5 v5.13.0`)
- **MongoDB**: `7.0` (`go.mongodb.org/mongo-driver v1.17.10`)
- **Queue**: Redis `7.0+` (`github.com/redis/go-redis/v9 v9.23.0`)
- **API Router**: `github.com/go-chi/chi/v5 v5.5.0`
- **OpenAPI**: `3.0.3` specification (`/openapi.yaml`)
- **Containerization**: Docker Compose with multi-stage non-root containers

---

## Architecture Overview

```
Telegram MTProto
      |
      v
telegram-collector  ---> MongoDB (Raw Messages & History)
      |
      v
Redis Streams (perso:telegram:messages)
      |
      v
message-worker
      |--> Persian Normalization & Price Parsing (NLP)
      |--> Seller / Buyer Classification
      |--> 5-Level Product Matching Engine
      |--> PostgreSQL (Offers, Price Observations, Aggregates)
      |--> Conversation Manager (Human Approval & Outreach)
      |
backend-api  <--->  Web Dashboard & REST Clients
```

---

## Quick Start (Docker Deployment)

### 1. Prerequisites
- Docker Engine `24+` and Docker Compose `v2+`
- (Optional for local development) Go `1.24+`

### 2. Configure Environment
Copy `.env.example` to `.env`:
```bash
cp .env.example .env
```

To run in demo/offline mode without Telegram credentials, keep `TELEGRAM_MOCK_MODE=true` (or leave `TELEGRAM_APP_ID=0`).

### 3. Launch the Complete Stack
```bash
docker compose up --build
```

This starts all 7 services:
1. `perso-backend-api`: REST API on port `8080`
2. `perso-telegram-collector`: Ingestion service
3. `perso-message-worker`: Redis stream processor
4. `perso-postgres`: PostgreSQL database on port `5432`
5. `perso-mongodb`: MongoDB database on port `27017`
6. `perso-redis`: Redis stream broker on port `6379`
7. `perso-frontend`: Web Dashboard on port `3000`

### 4. Access the Application
- **Web Dashboard**: [http://localhost:3000](http://localhost:3000)
- **API Health Check**: [http://localhost:8080/health](http://localhost:8080/health)
- **API Readiness Check**: [http://localhost:8080/ready](http://localhost:8080/ready)
- **OpenAPI 3.0 Spec**: [http://localhost:8080/openapi.yaml](http://localhost:8080/openapi.yaml)

---

## REST API Reference

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health` | Service health status |
| `GET` | `/ready` | Database & Redis connectivity probe |
| `GET` | `/openapi.yaml` | OpenAPI 3.0 specification |
| `GET` | `/api/v1/interests` | List user purchase interests |
| `POST` | `/api/v1/interests` | Register interest (`buy_enabled=true`) |
| `DELETE` | `/api/v1/interests/{id}` | Delete interest |
| `GET` | `/api/v1/watch-sources` | List monitored Telegram sources |
| `POST` | `/api/v1/watch-sources` | Add channel or group to watch list |
| `DELETE` | `/api/v1/watch-sources/{id}` | Remove watch source |
| `GET` | `/api/v1/products` | Catalog products with pagination |
| `GET` | `/api/v1/products/{id}` | Product details and aliases |
| `GET` | `/api/v1/products/{id}/statistics` | Price statistics (min, max, median, distribution) |
| `GET` | `/api/v1/offers` | Filter and rank seller offers (`sort_by=best_offer`) |
| `GET` | `/api/v1/offers/{id}` | Offer details with explainable score breakdown |
| `GET` | `/api/v1/conversations` | List conversation outreach workflows |
| `GET` | `/api/v1/conversations/{id}` | Conversation state and chat history |
| `POST` | `/api/v1/conversations/{id}/approve` | Approve seller outreach draft |
| `POST` | `/api/v1/conversations/{id}/pause` | Pause ongoing conversation |
| `POST` | `/api/v1/conversations/{id}/close` | Complete/close conversation |
| `GET` | `/api/v1/dashboard/summary` | Dashboard summary metrics |
| `GET` | `/api/v1/analytics/prices` | Price analytics and 30-day observations |
| `GET` | `/api/v1/analytics/offers` | Top scored marketplace deals |

---

## Local Development & CLI

You can run individual process modes or everything in one process:

```bash
# Build binary
make build

# Run unit and integration tests
make test

# Format and vet code
make lint

# Run all services concurrently in one process
./bin/perso all

# Run only the REST API server
./bin/perso api

# Run only the MTProto Telegram Collector
./bin/perso collector

# Run only the Message Worker
./bin/perso worker

# Run database migrations
./bin/perso migrate
```

---

## Automated Testing

Run the automated test suite:
```bash
make test
```

Unit tests cover:
- Persian normalization (`normalizer_test.go`)
- Persian and mixed number parsing (`numbers_test.go`)
- Price extraction & currency identification (`price_parser_test.go`)
- Seller / buyer / spam classification (`classifier_test.go`)
- End-to-end deterministic product extraction (`extractor_test.go`)
- 5-level product catalog matching (`matcher_test.go`)
- Explainable offer scoring (`scorer_test.go`)
- Conversation state machine transitions (`state_machine_test.go`)
- Prompt injection defense & LLM integration (`llm_test.go`)
- REST API HTTP routing (`api_test.go`)
- Telegram MTProto mock & AES session storage (`telegram_test.go`)
- Worker processing pipeline (`worker_test.go`)

---

## Compliance & Security Notice

- **Telegram Terms Compliance**: In accordance with Telegram API Terms of Service, group membership does not automatically grant permission to transmit message contents to third-party AI services. External LLM processing is disabled by default (`LLM_ENABLED=false`).
- **Data Protection**: Original Telegram messages are preserved intact without modification. Sessions are stored with AES-256-GCM encryption on isolated volumes and never packaged inside container images.
- **Human-in-the-Loop**: Direct messages to sellers require explicit opt-in or human review (`AWAITING_APPROVAL`) before initiation. The assistant always introduces itself as an automated system and honors opt-out requests immediately.
