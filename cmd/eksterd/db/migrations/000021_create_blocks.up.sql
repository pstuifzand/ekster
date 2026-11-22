-- A block belongs to an account and may optionally be restricted to one of
-- that account's channels.  It mirrors mutes, but a block also removes the
-- author's existing items and keeps future ones from being stored.
CREATE TABLE blocks (
    id integer GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id integer NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_id integer REFERENCES channels(id) ON DELETE CASCADE,
    url text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE UNIQUE INDEX blocks_account_url_unique
    ON blocks (user_id, url) WHERE channel_id IS NULL;
CREATE UNIQUE INDEX blocks_channel_url_unique
    ON blocks (user_id, channel_id, url) WHERE channel_id IS NOT NULL;
CREATE INDEX blocks_user_channel_url_idx ON blocks (user_id, channel_id, url);
