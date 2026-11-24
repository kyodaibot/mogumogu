-- Installed only in the disposable browser-test database.
WITH snapshot AS (
  INSERT INTO menu_snapshots (cafeteria_id, menu_date, version, checked_at, digest)
  SELECT id, (now() AT TIME ZONE 'Asia/Tokyo')::date, 1, now(), 'e2e-menu'
  FROM cafeterias WHERE id IN ('650111','650113')
  RETURNING id, cafeteria_id
)
INSERT INTO menu_items (snapshot_id, dish_code, name, categories, member_price_yen, status,
                        source_url, allergens, calories, protein_g, fat_g, carbs_g)
SELECT s.id, x.code, x.name, ARRAY['主菜'], 385, 'unmarked',
       'https://west2-univ.jp/sp/detail.php?t=' || s.cafeteria_id || '&c=' || x.code,
       x.allergens, 450, 20, 10, NULL
FROM snapshot s CROSS JOIN (VALUES ('101','E2E 焼き魚',ARRAY['egg','wheat_origin']),
                                   ('102','E2E 豆腐',ARRAY[]::text[])) x(code,name,allergens);

INSERT INTO menu_items (snapshot_id, dish_code, name, categories, member_price_yen, status,
                        source_url, allergens, calories, protein_g, fat_g, carbs_g)
SELECT s.id, x.code, x.name, ARRAY['朝食プレート'], 100, 'unmarked',
       'https://west2-univ.jp/sp/detail.php?t=650111&c=' || x.code,
       ARRAY[]::text[], x.kcal, 20, 10, 40
FROM menu_snapshots s CROSS JOIN (VALUES ('201','朝 和風',400),('202','朝 洋風',420)) x(code,name,kcal)
WHERE s.cafeteria_id='650111';

-- 吉田食堂 has not imported today's menu: it falls back to yesterday's (plans/menu-stale-fallback.md).
WITH snapshot AS (
  INSERT INTO menu_snapshots (cafeteria_id, menu_date, version, checked_at, digest)
  VALUES ('650112', (now() AT TIME ZONE 'Asia/Tokyo')::date - 1, 1, now() - interval '1 day', 'e2e-yesterday')
  RETURNING id
)
INSERT INTO menu_items (snapshot_id, dish_code, name, categories, member_price_yen, status,
                        source_url, allergens, calories, protein_g, fat_g, carbs_g)
SELECT id, '101', 'E2E 焼き魚', ARRAY['主菜'], 385, 'unmarked',
       'https://west2-univ.jp/sp/detail.php?t=650112&c=101', ARRAY['egg','wheat_origin'], 450, 20, 10, NULL
FROM snapshot;
