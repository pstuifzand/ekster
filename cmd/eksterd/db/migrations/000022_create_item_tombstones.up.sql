-- Retain removed identities without retaining the item content.
CREATE TABLE item_tombstones (
    channel_id integer NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    uid varchar(512) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT current_timestamp,
    PRIMARY KEY (channel_id, uid)
);
