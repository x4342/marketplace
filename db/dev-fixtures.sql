-- Fixed stale heartbeat for local verification of the worker's first update.
INSERT INTO worker_heartbeat (singleton, observed_at)
VALUES (true, '2000-01-01T00:00:00Z')
ON CONFLICT (singleton) DO NOTHING;

-- These fixture sellers have deliberately unusable password hashes.
INSERT INTO users (id, username, password_hash, created_at) VALUES
    ('14eb9dab-3667-4a33-b797-dd6fe368a0fb', 'workshop', decode(repeat('00', 49), 'hex'), '2026-10-01'),
    ('c9180107-eeb2-48df-b936-7c0f2569758c', 'papertrail', decode(repeat('00', 49), 'hex'), '2026-10-02')
ON CONFLICT (id) DO NOTHING;

INSERT INTO categories (slug, name) VALUES
    ('equipment', 'Equipment'), ('downloads', 'Downloads')
ON CONFLICT (slug) DO NOTHING;
INSERT INTO categories (slug, name, parent_slug) VALUES
    ('accessories', 'Accessories', 'equipment'), ('templates', 'Templates', 'downloads')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO listings (id, seller_id, category_slug, title, description, kind, price_minor, currency,
                      stock, can_backorder, condition, ship_regions, delivery_mode, terms, status, published_at)
VALUES
    ('892351c3-15c9-468e-a61f-b948658c18e5', '14eb9dab-3667-4a33-b797-dd6fe368a0fb',
     'accessories', 'Compact mechanical keyboard', 'Wired keyboard with a compact layout and tactile switches.',
     'physical', 5750, 'EUR', 4, false, 'used', ARRAY['EU'], '', 'Ships within two working days.', 'published', '2026-11-21'),
    ('678c69e7-4ab1-4971-8603-7125da01ae06', '14eb9dab-3667-4a33-b797-dd6fe368a0fb',
     'accessories', 'Cork desk mat', 'Cork keyboard mat cut to order in small batches.',
     'physical', 2400, 'USD', 0, true, 'new', ARRAY['EU','US'], '', 'Made to order in one week.', 'published', '2026-11-20'),
    ('3cd38877-43d7-403a-94b4-450456bde7ce', 'c9180107-eeb2-48df-b936-7c0f2569758c',
     'templates', 'Workshop inventory sheets', 'Printable inventory, reorder, and equipment sheets.',
     'digital', 900, 'USD', 100, false, '', ARRAY[]::text[], 'automatic', 'ZIP archive with printable PDFs.', 'published', '2026-11-19'),
    ('ced4424d-b34f-40a3-80cc-48933aab997c', 'c9180107-eeb2-48df-b936-7c0f2569758c',
     'templates', 'Private fixture draft', 'Used to verify that draft listings remain hidden.',
     'digital', 100, 'USD', 10, false, '', ARRAY[]::text[], 'manual', 'Draft.', 'draft', NULL)
ON CONFLICT (id) DO NOTHING;
