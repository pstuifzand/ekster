-- Media uploaded through the Micropub media endpoint. The bytes live in the
-- database so one backup covers the posts and the files they reference.
CREATE TABLE media (
    id integer PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    uid varchar(64) NOT NULL UNIQUE,
    channel_id integer NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    content_type varchar(255) NOT NULL,
    filename varchar(255) NOT NULL DEFAULT '',
    size integer NOT NULL,
    data bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT current_timestamp
);

CREATE INDEX media_channel_id ON media (channel_id);
