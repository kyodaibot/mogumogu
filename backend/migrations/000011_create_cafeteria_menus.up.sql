CREATE TABLE cafeterias (
    id text PRIMARY KEY,
    name text NOT NULL,
    campus text NOT NULL CHECK (campus IN ('yoshida', 'uji', 'katsura')),
    menu_url text NOT NULL
);

INSERT INTO cafeterias (id, name, campus, menu_url)
SELECT code, name, campus, 'https://west2-univ.jp/sp/menu.php?t=' || code
FROM (VALUES
    ('650111', '中央食堂', 'yoshida'), ('650112', '吉田食堂', 'yoshida'),
    ('650113', '北部食堂', 'yoshida'), ('650115', '南部食堂', 'yoshida'),
    ('650116', '宇治食堂', 'uji'), ('650118', 'カフェテリア・ルネ', 'yoshida'),
    ('650120', '桂セレネ', 'katsura')
) AS stores(code, name, campus);

CREATE TABLE cafeteria_hours (
    cafeteria_id text NOT NULL REFERENCES cafeterias,
    menu_date date NOT NULL,
    intervals jsonb CHECK (jsonb_typeof(intervals) = 'array'),
    is_holiday boolean NOT NULL DEFAULT false,
    source_url text NOT NULL,
    checked_at timestamptz NOT NULL,
    PRIMARY KEY (cafeteria_id, menu_date)
);

CREATE TABLE menu_snapshots (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    cafeteria_id text NOT NULL REFERENCES cafeterias,
    menu_date date NOT NULL,
    version integer NOT NULL CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    checked_at timestamptz NOT NULL,
    digest text NOT NULL,
    UNIQUE (cafeteria_id, menu_date, version)
);

CREATE TABLE menu_items (
    snapshot_id bigint NOT NULL REFERENCES menu_snapshots,
    dish_code text NOT NULL,
    name text NOT NULL,
    name_en text,
    categories text[] NOT NULL,
    member_price_yen integer NOT NULL CHECK (member_price_yen >= 0),
    status text NOT NULL CHECK (status IN ('unmarked','sold_out','standby','unknown')),
    source_url text NOT NULL,
    allergens text[],
    calories numeric NOT NULL CHECK (calories >= 0),
    protein_g numeric CHECK (protein_g >= 0),
    fat_g numeric CHECK (fat_g >= 0),
    carbs_g numeric CHECK (carbs_g >= 0),
    extra_nutrients jsonb NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (snapshot_id, dish_code)
);

CREATE TABLE menu_import_jobs (
    cafeteria_id text NOT NULL REFERENCES cafeterias,
    menu_date date NOT NULL,
    phase text NOT NULL CHECK (phase IN ('morning','review')),
    attempts integer NOT NULL CHECK (attempts > 0),
    next_retry_at timestamptz,
    last_error text,
    -- SQL NULL requests a new category listing; [] means the detail phase finished.
    pending_details jsonb,
    base_snapshot_id bigint REFERENCES menu_snapshots,
    hours_pending boolean NOT NULL,
    PRIMARY KEY (cafeteria_id, menu_date, phase)
);
