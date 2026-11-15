-- Healthcare catalog metadata and buyer filter persistence.
ALTER TABLE products ADD COLUMN IF NOT EXISTS unit TEXT NOT NULL DEFAULT '';
ALTER TABLE products ADD COLUMN IF NOT EXISTS pack_size INTEGER;
ALTER TABLE products ADD COLUMN IF NOT EXISTS prescription_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS expiry_required BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE interests ADD COLUMN IF NOT EXISTS max_price_amount BIGINT;
ALTER TABLE interests ADD COLUMN IF NOT EXISTS min_quantity INTEGER;
ALTER TABLE interests ADD COLUMN IF NOT EXISTS preferred_city TEXT NOT NULL DEFAULT '';
ALTER TABLE interests ADD COLUMN IF NOT EXISTS required_attributes JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE interests ADD CONSTRAINT interests_min_quantity_positive CHECK (min_quantity IS NULL OR min_quantity > 0);
ALTER TABLE interests ADD CONSTRAINT interests_max_price_nonnegative CHECK (max_price_amount IS NULL OR max_price_amount >= 0);
CREATE INDEX IF NOT EXISTS idx_interests_filters ON interests(user_id, product_id, max_price_amount, min_quantity);
