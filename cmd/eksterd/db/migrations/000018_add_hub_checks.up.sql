-- Discovery is retried on its own schedule, not on every renewal pass: a feed
-- that declares no WebSub hub will never declare one, so asking it every minute
-- buys nothing and costs its host two requests a minute. These two columns
-- remember the last attempt and how many there have been.
ALTER TABLE "subscriptions" ADD COLUMN IF NOT EXISTS "hub_checks" integer NOT NULL DEFAULT 0;
ALTER TABLE "subscriptions" ADD COLUMN IF NOT EXISTS "hub_checked_at" timestamptz;
