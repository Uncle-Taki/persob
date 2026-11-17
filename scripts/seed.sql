-- Explicit synthetic healthcare catalog fixtures for demo/integration only.
INSERT INTO users (id, username, email, full_name, role)
VALUES ('11111111-1111-1111-1111-111111111111', 'demo_buyer', 'buyer@perso.local', 'کاربر آزمایشی پرسو', 'buyer')
ON CONFLICT (id) DO NOTHING;
INSERT INTO products (id,name,normalized_name,brand,manufacturer,model,category,attributes) VALUES
('22222222-2222-2222-2222-222222222221','سرم نرمال سالین ۰٫۹٪','سرم نرمال سالین 0.9','داروسازی ایرانی','داروسازی ایرانی','500ml','medical_consumables','{"concentration":"0.9%","volume":"500ml","form":"solution"}'),
('22222222-2222-2222-2222-222222222222','دستکش معاینه نیتریل','دستکش معاینه نیتریل','مدیکال','تولیدکننده نمونه','Medium','medical_consumables','{"material":"nitrile","size":"M"}'),
('22222222-2222-2222-2222-222222222223','فشارسنج دیجیتال بازویی','فشارسنج دیجیتال بازویی','هلث‌تک','تولیدکننده نمونه','Arm BP Monitor','health_monitoring','{"measurement":"upper_arm"}'),
('22222222-2222-2222-2222-222222222224','ماسک سه‌لایه پزشکی','ماسک سه لایه پزشکی','مدیکال','تولیدکننده نمونه','3-ply','medical_consumables','{"layers":3}'),
('22222222-2222-2222-2222-222222222225','محلول ضدعفونی دست ۵۰۰ میلی‌لیتر','محلول ضدعفونی دست 500','بهداشت','تولیدکننده نمونه','500ml','hygiene_products','{"volume":"500ml"}')
ON CONFLICT (id) DO NOTHING;
INSERT INTO product_aliases (product_id,alias,normalized_alias,locale,alias_type) VALUES
('22222222-2222-2222-2222-222222222221','سرم نرمال سالین','سرم نرمال سالین','fa','persian_name'),
('22222222-2222-2222-2222-222222222221','نرمال سالین','نرمال سالین','fa','generic_name'),
('22222222-2222-2222-2222-222222222221','Normal Saline 0.9%','normal saline 0.9','en','english_name'),
('22222222-2222-2222-2222-222222222222','دستکش نیتریل','دستکش نیتریل','fa','persian_name'),
('22222222-2222-2222-2222-222222222223','دستگاه فشار خون دیجیتال','دستگاه فشار خون دیجیتال','fa','persian_name')
ON CONFLICT DO NOTHING;
INSERT INTO interests (user_id,product_id,sell_enabled,buy_enabled,max_price_amount,min_quantity,preferred_city)
VALUES ('11111111-1111-1111-1111-111111111111','22222222-2222-2222-2222-222222222221',false,true,30000,12,'تهران')
ON CONFLICT DO NOTHING;
INSERT INTO watch_sources (user_id,telegram_chat_id,username,title,source_type,sell_enabled,buy_enabled,monitoring_status)
VALUES ('11111111-1111-1111-1111-111111111111',-1001234567890,'sample_healthcare','بازار آزمایشی سلامت','channel',false,true,'active')
ON CONFLICT DO NOTHING;
INSERT INTO telegram_sources (id,owner_id,telegram_peer_id,title,username,source_type,access_type,expected_type,detected_type,capabilities,is_active,ingestion_status)
VALUES ('33333333-3333-3333-3333-333333333331','11111111-1111-1111-1111-111111111111',-1001234567890,'بازار آزمایشی سلامت','sample_healthcare','broadcast_channel','public','broadcast_channel','broadcast_channel','{"can_read_history":true,"has_topics":false,"has_discussion":false,"sender_metadata_available":true}',true,'active')
ON CONFLICT (workspace_id,telegram_peer_id) DO NOTHING;
