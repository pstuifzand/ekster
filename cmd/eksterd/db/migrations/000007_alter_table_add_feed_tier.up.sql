alter table "feeds"
    add column "tier" int default 0,
    add column "unmodified" int default 0,
    add column "next_fetch_at" timestamptz;
