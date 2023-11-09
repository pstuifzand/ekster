BEGIN;
CREATE OR REPLACE FUNCTION update_timestamp()
    RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';
COMMIT;

CREATE TABLE "sources" (
   "id" int primary key generated always as identity,
   "channel_id" int not null,
   "auth_code" varchar(64) not null,
   "created_at" timestamp DEFAULT current_timestamp,
   "updated_at" timestamp DEFAULT current_timestamp
);

CREATE TRIGGER sources_update_timestamp BEFORE INSERT OR UPDATE ON "sources"
FOR EACH ROW EXECUTE PROCEDURE update_timestamp();
