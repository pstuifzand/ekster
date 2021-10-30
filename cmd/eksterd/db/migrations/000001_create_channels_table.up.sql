CREATE TABLE IF NOT EXISTS "channels" (
    "id" int primary key generated always as identity,
    "uid" varchar(255) unique,
    "name" varchar(255) unique,
    "created_at" timestamptz DEFAULT current_timestamp,
    "updated_at" timestamptz
);
