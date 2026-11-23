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

INSERT INTO cafeteria_hours (cafeteria_id,menu_date,intervals,source_url,checked_at)
SELECT id, (now() AT TIME ZONE 'Asia/Tokyo')::date,
       '[{"start":"08:00","end":"20:00"}]', 'https://www.s-coop.net/shop_guide/yoshida_shop/', now()
FROM cafeterias WHERE id IN ('650111','650113');

INSERT INTO menu_items (snapshot_id, dish_code, name, categories, member_price_yen, status,
                        source_url, allergens, calories, protein_g, fat_g, carbs_g)
SELECT s.id, x.code, x.name, ARRAY['朝食プレート'], 100, 'unmarked',
       'https://west2-univ.jp/sp/detail.php?t=650111&c=' || x.code,
       ARRAY[]::text[], x.kcal, 20, 10, 40
FROM menu_snapshots s CROSS JOIN (VALUES ('201','朝 和風',400),('202','朝 洋風',420)) x(code,name,kcal)
WHERE s.cafeteria_id='650111';
