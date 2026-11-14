-- 002_telegram_sources_and_marketplace.sql
-- Normalized Telegram Source Registry, Seller Profiles, and Buying Opportunity Match Results

-- Telegram Sources (Normalized Registry supporting Basic Group, Supergroup, Forum, Broadcast Channel)
CREATE TABLE IF NOT EXISTS telegram_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    telegram_peer_id BIGINT NOT NULL,
    telegram_access_hash BIGINT NOT NULL DEFAULT 0,
    username VARCHAR(100),
    title VARCHAR(255) NOT NULL,
    source_type VARCHAR(50) NOT NULL, -- basic_group, supergroup, forum, broadcast_channel
    access_type VARCHAR(20) NOT NULL DEFAULT 'public', -- public, private
    expected_type VARCHAR(50),
    detected_type VARCHAR(50) NOT NULL,
    linked_discussion_peer_id BIGINT,
    capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    is_active BOOLEAN NOT NULL DEFAULT true,
    ingestion_status VARCHAR(50) NOT NULL DEFAULT 'active', -- active, paused, error, unauthorized
    last_message_id BIGINT NOT NULL DEFAULT 0,
    last_synced_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_telegram_sources_owner_peer UNIQUE (owner_id, telegram_peer_id)
);

CREATE INDEX IF NOT EXISTS idx_telegram_sources_owner ON telegram_sources(owner_id);
CREATE INDEX IF NOT EXISTS idx_telegram_sources_peer ON telegram_sources(telegram_peer_id);
CREATE INDEX IF NOT EXISTS idx_telegram_sources_status ON telegram_sources(ingestion_status);
CREATE INDEX IF NOT EXISTS idx_telegram_sources_type ON telegram_sources(source_type);

-- Seller Profiles (Structured directory of sellers, rate limits, opt-outs, and reputation)
CREATE TABLE IF NOT EXISTS seller_profiles (
    seller_id BIGINT PRIMARY KEY,
    username VARCHAR(100),
    display_name VARCHAR(255),
    city VARCHAR(100),
    tehran_area VARCHAR(100),
    total_offers INT NOT NULL DEFAULT 0,
    opted_out BOOLEAN NOT NULL DEFAULT false,
    cooldown_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_seller_profiles_username ON seller_profiles(username);
CREATE INDEX IF NOT EXISTS idx_seller_profiles_opted_out ON seller_profiles(opted_out);

-- Offer Match Results (Traceable link between buyer interests and seller offers)
CREATE TABLE IF NOT EXISTS offer_match_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    offer_id UUID NOT NULL REFERENCES offers(id) ON DELETE CASCADE,
    interest_id UUID REFERENCES interests(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    match_score NUMERIC(5,2) NOT NULL,
    match_explanation TEXT,
    matched_attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    unmatched_attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_offer_match_results_user ON offer_match_results(user_id);
CREATE INDEX IF NOT EXISTS idx_offer_match_results_offer ON offer_match_results(offer_id);
CREATE INDEX IF NOT EXISTS idx_offer_match_results_product ON offer_match_results(product_id);

-- Buy Offers View (Normalized active buying opportunities)
CREATE OR REPLACE VIEW buy_offers AS
SELECT
    id, product_id, source_message_id, seller_id, brand, manufacturer, model,
    condition, quantity, price_amount, price_currency, price_raw, price_confidence,
    branded, expiration_date, seller_city, seller_area, seller_location_text,
    listing_status, extraction_confidence, created_at, updated_at
FROM offers
WHERE listing_status = 'active';
