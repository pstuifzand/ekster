CREATE TABLE "subscriptions" (
     "id" int primary key generated always as identity,
     "topic" varchar(1024) not null references "feeds" ("url") on update cascade on delete cascade,
     "hub" varchar(1024) null,
     "callback" varchar(1024) null,
     "subscription_secret" varchar(32) not null,
     "url_secret" varchar(32) not null,
     "lease_seconds" int not null,
     "created_at" timestamptz DEFAULT current_timestamp,
     "updated_at" timestamptz,
     "resubscribe_at" timestamptz
);
