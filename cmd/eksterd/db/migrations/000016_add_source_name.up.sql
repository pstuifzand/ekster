ALTER TABLE "sources"
    ADD COLUMN "name" varchar(128) NOT NULL DEFAULT '';

ALTER TABLE "sources"
    ADD COLUMN "last_used_at" timestamp with time zone;
