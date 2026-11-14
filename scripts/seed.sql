-- Seed canonical products, aliases and default demo user
INSERT INTO users (id, username, email, full_name, role)
VALUES
    ('11111111-1111-1111-1111-111111111111', 'demo_buyer', 'buyer@perso.local', 'کاربر تستی پرسو', 'user')
ON CONFLICT (id) DO NOTHING;

-- Products
INSERT INTO products (id, name, normalized_name, brand, manufacturer, model, category, attributes)
VALUES
    ('22222222-2222-2222-2222-222222222221', 'iPhone 15 Pro', 'iphone 15 pro', 'Apple', 'Apple Inc.', '15 Pro', 'Smartphone', '{"storage": ["128GB", "256GB", "512GB", "1TB"], "sim": ["zaa", "ch", "ll"]}'::jsonb),
    ('22222222-2222-2222-2222-222222222222', 'iPhone 15 Pro Max', 'iphone 15 pro max', 'Apple', 'Apple Inc.', '15 Pro Max', 'Smartphone', '{"storage": ["256GB", "512GB", "1TB"], "sim": ["zaa", "ch"]}'::jsonb),
    ('22222222-2222-2222-2222-222222222223', 'Samsung Galaxy S24 Ultra', 'samsung galaxy s24 ultra', 'Samsung', 'Samsung Electronics', 'S24 Ultra', 'Smartphone', '{"storage": ["256GB", "512GB", "1TB"], "ram": "12GB"}'::jsonb),
    ('22222222-2222-2222-2222-222222222224', 'MacBook Pro M3 Pro', 'macbook pro m3 pro', 'Apple', 'Apple Inc.', 'MacBook Pro 14"', 'Laptop', '{"chip": "M3 Pro", "ram": "18GB", "ssd": "512GB"}'::jsonb),
    ('22222222-2222-2222-2222-222222222225', 'Sony PlayStation 5 Slim', 'sony playstation 5 slim', 'Sony', 'Sony Interactive Entertainment', 'PS5 Slim', 'Gaming Console', '{"edition": ["digital", "standard"]}'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- Product Aliases
INSERT INTO product_aliases (product_id, alias, normalized_alias, locale, alias_type)
VALUES
    ('22222222-2222-2222-2222-222222222221', 'آیفون ۱۵ پرو', 'آیفون ۱۵ پرو', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222221', 'ایفون 15 پرو', 'ایفون 15 پرو', 'fa', 'persian_spelling'),
    ('22222222-2222-2222-2222-222222222221', 'iPhone 15Pro', 'iphone 15pro', 'en', 'abbreviation'),
    ('22222222-2222-2222-2222-222222222222', 'آیفون ۱۵ پرومکس', 'آیفون ۱۵ پرومکس', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222222', 'ایفون ۱۵ پرو مکس', 'ایفون ۱۵ پرو مکس', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222223', 'سامسونگ اس ۲۴ اولترا', 'سامسونگ اس ۲۴ اولترا', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222223', 'S24 Ultra', 's24 ultra', 'en', 'short_model'),
    ('22222222-2222-2222-2222-222222222224', 'مک بوک پرو ام ۳', 'مک بوک پرو ام ۳', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222225', 'پلی استیشن ۵ اسلیم', 'پلی استیشن ۵ اسلیم', 'fa', 'persian_name'),
    ('22222222-2222-2222-2222-222222222225', 'PS5 Slim', 'ps5 slim', 'en', 'short_model')
ON CONFLICT DO NOTHING;

-- Initial Interest for demo_buyer: iPhone 15 Pro
INSERT INTO interests (user_id, product_id, sell_enabled, buy_enabled)
VALUES ('11111111-1111-1111-1111-111111111111', '22222222-2222-2222-2222-222222222221', false, true)
ON CONFLICT DO NOTHING;

-- Initial Watch Source for demo_buyer: Example marketplace channel
INSERT INTO watch_sources (user_id, telegram_chat_id, username, title, source_type, sell_enabled, buy_enabled, monitoring_status)
VALUES ('11111111-1111-1111-1111-111111111111', -1001234567890, 'iran_tech_bazaar', 'بازار موبایل و لپتاپ پایتخت', 'channel', false, true, 'active')
ON CONFLICT DO NOTHING;

-- Seed initial telegram source matching existing watch source for demo buyer
INSERT INTO telegram_sources (
    id, owner_id, telegram_peer_id, title, username, source_type, access_type,
    expected_type, detected_type, linked_discussion_peer_id, capabilities, is_active, ingestion_status
)
VALUES (
    '33333333-3333-3333-3333-333333333331',
    '11111111-1111-1111-1111-111111111111',
    -1001234567890,
    'بازار موبایل و لپتاپ پایتخت',
    'iran_tech_bazaar',
    'broadcast_channel',
    'public',
    'broadcast_channel',
    'broadcast_channel',
    -1001234567899,
    '{"can_read_history": true, "has_topics": false, "has_discussion": true, "sender_metadata_available": false}'::jsonb,
    true,
    'active'
)
ON CONFLICT (owner_id, telegram_peer_id) DO NOTHING;
