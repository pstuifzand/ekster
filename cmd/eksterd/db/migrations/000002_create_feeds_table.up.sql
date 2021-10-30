CREATE TABLE "feeds" (
     "id" int primary key generated always as identity,
     "channel_id" int references "channels" on update cascade on delete cascade,
     "url" varchar(512) not null unique,
     "created_at" timestamptz DEFAULT current_timestamp,
     "updated_at" timestamptz
);
