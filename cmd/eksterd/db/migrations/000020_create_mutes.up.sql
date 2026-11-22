-- A mute belongs to an account and may optionally be restricted to one of
-- that account's channels.  Keep channel UIDs globally unique: application
-- code can then safely carry a UID in a feed refresh, while every request
-- that comes from a user still verifies its owner explicitly.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'channels_uid_key'
    ) THEN
        ALTER TABLE channels ADD CONSTRAINT channels_uid_key UNIQUE (uid);
    END IF;
END $$;

CREATE TABLE mutes (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id integer REFERENCES channels(id) ON DELETE CASCADE,
    url text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE UNIQUE INDEX mutes_account_url_unique
    ON mutes (user_id, url) WHERE channel_id IS NULL;
CREATE UNIQUE INDEX mutes_channel_url_unique
    ON mutes (user_id, channel_id, url) WHERE channel_id IS NOT NULL;
CREATE INDEX mutes_user_channel_url_idx ON mutes (user_id, channel_id, url);
