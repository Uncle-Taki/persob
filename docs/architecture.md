# PERSO Architectural Specification & Technical Reference

## 1. System Overview

Perso is an intelligent marketplace monitoring and price-discovery platform initially targeting the Iranian electronic equipment market (smartphones, laptops, consoles). It ingests authorized Telegram channel and group messages, deterministically normalizes Persian text, extracts commercial offers and pricing (with optional LLM enrichment under strict compliance and prompt injection guards), matches offers against user interests, and supports human-supervised outreach workflows.

```
                    +---------------------------+
                    |  Telegram Network (MTProto)|
                    +-------------+-------------+
                                  |
                                  v
                    +---------------------------+
                    |   telegram-collector      |
                    |  (gotd/td MTProto Client) |
                    +------+--------------+-----+
                           |              |
                           v              v
     +--------------------------+    +-----------------------+
     |   MongoDB Raw Store      |    |  Redis Streams Queue  |
     | (telegram_messages &     |    | (perso:telegram:      |
     |  conversation histories) |    |  messages)            |
     +--------------------------+    +-----------+-----------+
                                                 |
                                                 v
                                    +------------------------+
                                    |    message-worker      |
                                    +------------+-----------+
                                                 |
         +---------------------------------------+---------------------------------------+
         |                                       |                                       |
         v                                       v                                       v
+------------------+                   +--------------------+                  +------------------+
| Persian NLP &    |                   | Product Catalog &  |                  | Explainable Best |
| Deterministic    |                   | Interest Matching  |                  | Offer Scorer     |
| Extraction       |                   | (5-level matching) |                  | & Aggregator     |
+--------+---------+                   +---------+----------+                  +--------+---------+
         |                                       |                                      |
         |         +-----------------------------+--------------------------------------+
         v         v
+-------------------------------+                     +---------------------------------------+
|  PostgreSQL Marketplace DB    | <=================> |  backend-api (REST & OpenAPI 3.0)     |
|  (products, offers, prices,   |                     |  /api/v1/interests                    |
|   interests, conversations)   |                     |  /api/v1/offers                       |
+---------------+---------------+                     |  /api/v1/analytics                    |
                |                                     +-------------------+-------------------+
                v                                                         |
+-------------------------------+                                         v
|     Conversation Manager      |                             +-----------------------+
| (Strict Opt-in, Auto/Approval |                             |  Frontend Dashboard   |
|  state machine, LLM Provider) |                             |  (Nginx / HTML5 RTL)  |
+-------------------------------+                             +-----------------------+
```

---

## 2. Ingestion & MTProto Component (`internal/telegram/`)

1. **Authentication & Persistent Storage**:
   - Built on `github.com/gotd/td`.
   - MTProto session data is encrypted at rest using AES-256-GCM (`EncryptedSessionStorage`) with nonces generated via `crypto/rand`.
   - Implements file-level mutual exclusion (`syscall.Flock`) to guarantee that only one collector instance can access a session file at any time.

2. **Compliance & Privacy Guardrails**:
   - Collector queries `watch_sources` in PostgreSQL and only processes updates from chat IDs that the user has explicitly configured (`monitoring_status = 'active'`).
   - Group membership is never assumed to grant permission for external transmission.
   - External LLM processing is disabled by default (`LLM_ENABLED=false`) until explicitly verified and enabled.

3. **Flood Wait & Network Resilience**:
   - `HandleFloodWait` intercepts Telegram API rate limits, parsing sleep durations and backing off without crashing.

4. **Testing & Mock Client**:
   - Includes `MockClient` for deterministic offline testing and container startup in development mode (`TELEGRAM_MOCK_MODE=true`).

---

## 3. Storage Layer (`internal/storage/`)

### MongoDB (Raw & Dynamic Telemetry)
- `telegram_messages`: Preserves original Telegram messages byte-for-byte with unique compound index on `(telegram_chat_id, telegram_message_id)`.
- `telegram_updates`: Raw Telegram event log.
- `extracted_messages`: Structured extraction records.
- `conversation_messages`: Complete conversation transcript between assistant and seller.
- `processing_jobs`: Worker job tracking and failure recovery.

### PostgreSQL (Structured Catalog & Analytics)
- `users`: Application users and roles.
- `products`: Canonical products catalog with JSONB attributes.
- `product_aliases`: Persian and English aliases and spellings.
- `interests`: User interest list (Phase 1: `buy_enabled=true`, `sell_enabled=false`).
- `watch_sources`: Monitored Telegram channels/groups.
- `offers`: Structured marketplace offers linked to products.
- `price_observations`: Historical price points.
- `price_aggregates`: Precomputed statistics (min, max, average, median, distribution buckets).
- `conversations`: Conversation state machine and collected field data.
- `audit_logs`: Audit trail for human approvals and critical actions.

---

## 4. Persian NLP & Price Extraction (`internal/nlp/`, `internal/extraction/`)

1. **Character Unification**:
   - Arabic Yeh (`ي`, `ى`) -> Persian Yeh (`ی`).
   - Arabic Kaf (`ك`) -> Persian Kaf (`ک`).
   - Arabic Teh Marbuta (`ة`) -> `ه`.
   - Stripping diacritics and tatweel.
   - Persian (`۰-۹`) and Arabic (`٠-٩`) digits converted to standard ASCII digits.

2. **Price Extraction**:
   - Multipliers: `میلیون`, `میلیارد`, `هزار`, `همت`, and shorthand `م`, `k`, `ت`.
   - Mixed compound expressions: `۹۸ میلیون و ۵۰۰ هزار تومان`.
   - Thousand-separator formatted digits: `۹۸,۵۰۰,۰۰۰ تومان`.
   - Currency isolation: `IRR_TOMAN` vs `IRR_RIAL`.
   - Non-price exclusions: Avoids false positives from model numbers (e.g. `15`), storage (`256 گیگ`), battery health (`88%`), and dates (`1403`).

3. **Classification**:
   - Deterministic classification into `Seller`, `Buyer`, `Neither`, and `Uncertain`.
   - Uncertain messages are preserved without forcing false positives.
   - Buyer requests are classified but prevented from becoming seller offers.

---

## 5. Product Matching Engine (`internal/matching/`)

Matches seller advertisements against catalog products using 5 progressive levels:
1. `Exact ID` (confidence 1.0)
2. `Exact Normalized Name` (confidence 0.98)
3. `Product Aliases` (confidence 0.95)
4. `Brand and Model Matching` (confidence 0.90)
5. `Multi-keyword Matching` (confidence 0.82)

**Variant Matching**: Differentiates capacity and configurations (e.g. `256GB` vs `128GB`) so distinct variants are not collapsed.

---

## 6. Conversation Manager & Human-in-the-Loop (`internal/conversation/`)

1. **Purpose**: Inquire about missing fields:
   - Price
   - Branded/Company authenticity
   - Manufacturer / Part number
   - Expiration / Warranty date
   - Seller City
   - Tehran neighborhood / Bazaar location (never exact home address)

2. **State Machine**:
   `NEW` -> `AWAITING_APPROVAL` (human review gate) -> `AWAITING_CONSENT` -> `ACTIVE` -> `WAITING_FOR_REPLY` -> `COMPLETED`, `DECLINED`, `OPTED_OUT`, `PAUSED`.

3. **Safety & Identity**:
   - Introduces itself as Perso's automated marketplace assistant.
   - Discloses AI identity truthfully when asked ("رباتی؟").
   - Refusal or opt-out is honored immediately without further messages.
   - Never negotiates or makes purchasing commitments.

---

## 7. LLM Provider & Prompt Defense (`internal/llm/`)

- Abstract `Provider` interface supporting Gemini (`gemini-2.5-flash`) and offline `MockProvider`.
- Strict JSON Schema output mode.
- **Prompt Injection Defense**: Untrusted message content is encapsulated inside `<untrusted_marketplace_message>` XML tags, with system prompts enforcing passive analysis.
- **Deterministic Precedence**: LLM output can enrich missing fields but cannot overwrite high-confidence deterministically extracted prices without explicit human review.

---

## 8. Offer Scoring & Price Analytics (`internal/analytics/`)

1. **Explainable Best-Offer Scoring (0 to 100)**:
   - Price Competitiveness (35% weight): Relative to median market price.
   - Product Condition (20% weight): `new` (1.0), `like_new` (0.85), `used_good` (0.70), `used_fair` (0.50), `used_poor` (0.20).
   - Offer Freshness (15% weight): Exponential decay based on age in hours.
   - Extraction Confidence (10% weight): Confidence score from extraction.
   - Variant Match (10% weight): Accuracy of hardware configuration.
   - Location Match (10% weight): Proximity to primary technology markets.

2. **Statistical Outlier Rejection**:
   - Interquartile Range (IQR) filtering: Eliminates erroneous listings outside `[Q1 - 1.5*IQR, Q3 + 1.5*IQR]`.
   - Computes Min, Max, Average, Median, Price Range, 5-bucket distribution histogram, and 48-hour trend.
