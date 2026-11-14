-- 001_initial_schema.sql: Initial PostgreSQL schema for Perso Marketplace Platform

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public;

-- Users
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(100) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE,
    full_name VARCHAR(255),
    role VARCHAR(50) NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Products
CREATE TABLE IF NOT EXISTS products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    normalized_name VARCHAR(255) NOT NULL,
    brand VARCHAR(100) NOT NULL,
    manufacturer VARCHAR(100),
    model VARCHAR(100) NOT NULL,
    category VARCHAR(100) NOT NULL,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_products_normalized_name ON products(normalized_name);
CREATE INDEX IF NOT EXISTS idx_products_brand_model ON products(brand, model);
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);

-- Product Aliases
CREATE TABLE IF NOT EXISTS product_aliases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    alias VARCHAR(255) NOT NULL,
    normalized_alias VARCHAR(255) NOT NULL,
    locale VARCHAR(10) NOT NULL DEFAULT 'fa',
    alias_type VARCHAR(50) NOT NULL DEFAULT 'alternative_name',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_product_aliases_normalized ON product_aliases(normalized_alias);
CREATE INDEX IF NOT EXISTS idx_product_aliases_product_id ON product_aliases(product_id);

-- Interests (User interest list)
CREATE TABLE IF NOT EXISTS interests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    sell_enabled BOOLEAN NOT NULL DEFAULT false,
    buy_enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_product_interest UNIQUE (user_id, product_id)
);

CREATE INDEX IF NOT EXISTS idx_interests_user_id ON interests(user_id);
CREATE INDEX IF NOT EXISTS idx_interests_product_id ON interests(product_id);

-- Watch Sources (Telegram channels and groups)
CREATE TABLE IF NOT EXISTS watch_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_chat_id BIGINT NOT NULL,
    username VARCHAR(100),
    title VARCHAR(255) NOT NULL,
    source_type VARCHAR(50) NOT NULL DEFAULT 'channel',
    sell_enabled BOOLEAN NOT NULL DEFAULT false,
    buy_enabled BOOLEAN NOT NULL DEFAULT true,
    monitoring_status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_chat_source UNIQUE (user_id, telegram_chat_id)
);

CREATE INDEX IF NOT EXISTS idx_watch_sources_user_id ON watch_sources(user_id);
CREATE INDEX IF NOT EXISTS idx_watch_sources_telegram_chat ON watch_sources(telegram_chat_id);
CREATE INDEX IF NOT EXISTS idx_watch_sources_status ON watch_sources(monitoring_status);

-- Offers (Structured seller advertisements)
CREATE TABLE IF NOT EXISTS offers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    source_message_id VARCHAR(100) NOT NULL,
    seller_id BIGINT NOT NULL,
    brand VARCHAR(100),
    manufacturer VARCHAR(100),
    model VARCHAR(100),
    condition VARCHAR(50) NOT NULL DEFAULT 'unknown',
    quantity INT NOT NULL DEFAULT 1,
    price_amount BIGINT,
    price_currency VARCHAR(20) NOT NULL DEFAULT 'IRR_TOMAN',
    price_raw VARCHAR(255),
    price_confidence NUMERIC(4,3) NOT NULL DEFAULT 0.0,
    branded BOOLEAN NOT NULL DEFAULT true,
    expiration_date TIMESTAMPTZ,
    seller_city VARCHAR(100),
    seller_area VARCHAR(100),
    seller_location_text TEXT,
    listing_status VARCHAR(50) NOT NULL DEFAULT 'active',
    extraction_confidence NUMERIC(4,3) NOT NULL DEFAULT 0.0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_source_message UNIQUE (source_message_id)
);

CREATE INDEX IF NOT EXISTS idx_offers_product_id ON offers(product_id);
CREATE INDEX IF NOT EXISTS idx_offers_seller_id ON offers(seller_id);
CREATE INDEX IF NOT EXISTS idx_offers_price ON offers(price_amount) WHERE price_amount IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_offers_listing_status ON offers(listing_status);
CREATE INDEX IF NOT EXISTS idx_offers_created_at ON offers(created_at DESC);

-- Price Observations (Historical price points)
CREATE TABLE IF NOT EXISTS price_observations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    offer_id UUID REFERENCES offers(id) ON DELETE SET NULL,
    price_amount BIGINT NOT NULL,
    price_currency VARCHAR(20) NOT NULL DEFAULT 'IRR_TOMAN',
    observed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_price_observations_product_observed ON price_observations(product_id, observed_at DESC);

-- Price Aggregates
CREATE TABLE IF NOT EXISTS price_aggregates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    min_price BIGINT NOT NULL DEFAULT 0,
    max_price BIGINT NOT NULL DEFAULT 0,
    avg_price BIGINT NOT NULL DEFAULT 0,
    median_price BIGINT NOT NULL DEFAULT 0,
    price_range BIGINT NOT NULL DEFAULT 0,
    sample_count INT NOT NULL DEFAULT 0,
    currency VARCHAR(20) NOT NULL DEFAULT 'IRR_TOMAN',
    period VARCHAR(20) NOT NULL DEFAULT 'all_time',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_product_period_currency UNIQUE (product_id, period, currency)
);

CREATE INDEX IF NOT EXISTS idx_price_aggregates_product ON price_aggregates(product_id);

-- Conversations (State machine & consent records)
CREATE TABLE IF NOT EXISTS conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offer_id UUID REFERENCES offers(id) ON DELETE SET NULL,
    seller_id BIGINT NOT NULL,
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    state VARCHAR(50) NOT NULL DEFAULT 'NEW',
    consent_granted BOOLEAN NOT NULL DEFAULT false,
    approved_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    approved_at TIMESTAMPTZ,
    questions_asked_count INT NOT NULL DEFAULT 0,
    max_questions INT NOT NULL DEFAULT 3,
    missing_fields JSONB NOT NULL DEFAULT '[]'::jsonb,
    collected_data JSONB NOT NULL DEFAULT '{}'::jsonb,
    notes TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_seller ON conversations(seller_id);
CREATE INDEX IF NOT EXISTS idx_conversations_state ON conversations(state);
CREATE INDEX IF NOT EXISTS idx_conversations_offer ON conversations(offer_id);

-- Audit Logs
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id VARCHAR(100) NOT NULL,
    action VARCHAR(100) NOT NULL,
    entity_type VARCHAR(100) NOT NULL,
    entity_id VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address VARCHAR(50),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action);
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
