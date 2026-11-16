# OpenAI-compatible demo provider (step 5)

Step 5 adds a server-side HTTP provider adapter and a disposable local mock. The adapter is selected with `LLM_PROVIDER=openai_compatible`; it never accepts a destination, model, or credential from browser input or marketplace text.

## Contract

- `GET /health` returns `{ "status": "ok" }`.
- `GET /v1/models` returns the `perso-healthcare-mock` model and requires `Authorization: Bearer $LLM_MOCK_TOKEN` when a token is configured.
- `POST /v1/chat/completions` accepts non-streaming `model`, `messages`, and optional `response_format: {"type":"json_object"}`. Tools and streaming are rejected explicitly.
- Successful responses contain an OpenAI-style `choices[0].message.content` and simulated `usage` counts.
- The client retries bounded 429, 5xx, and transport failures. Malformed JSON, missing content, unsupported status codes, timeouts, and invalid evidence fail closed.

The extraction adapter validates the response against the existing LLM schema and checks every product, price, and location evidence span against the original message. Conversation question generation falls back to the deterministic question when the provider fails. Consent remains deterministic application policy.

## Local demo

The demo Compose overlay enables the provider and points the worker at `http://llm-mock:8091/v1` with model `perso-healthcare-mock`. The mock returns different deterministic outputs for the synthetic Persian healthcare fixture (`سرم نرمال سالین`), an iPhone fixture, and unknown text. Set `ENVIRONMENT=development` or `test` and `LLM_MOCK_SCENARIO` to `rate_limit`, `server_error`, `malformed`, or `timeout` to exercise failure handling. Failure scenarios are refused in production.

The mock is synthetic infrastructure only. It does not call an external provider, store messages, or send seller communications.

Step 8 isolates this service to `compose.demo.yaml`; production Compose has no mock dependency. Demo setup generates a private `LLM_MOCK_TOKEN`. Canonical migrations finish before the demo seed runs. `TELEGRAM_MOCK_HEALTHCARE=true` selects the synthetic saline message only in development/test with mock Telegram mode. The release rehearsal requires a persisted successful `openai-compatible (perso-healthcare-mock)` extraction and no outbound messages.
