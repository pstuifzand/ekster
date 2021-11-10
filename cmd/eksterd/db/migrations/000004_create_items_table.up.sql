CREATE TABLE IF NOT EXISTS "items" (
    "id" int primary key generated always as identity,
    "channel_id" int references "channels" on delete cascade,
    "uid" varchar(512) not null unique,
    "is_read" int default 0,
    "data" jsonb,
    "created_at" timestamptz DEFAULT current_timestamp,
    "updated_at" timestamptz,
    "published_at" timestamptz
);
