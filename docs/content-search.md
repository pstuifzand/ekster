# Content search

Microsub `action=search` with a `channel` parameter searches posts; without
`channel` it searches for feeds. Use GET query parameters or a form-encoded
POST. The channel may be a channel UID or `global` (all channels owned by the
authenticated user). Supply at least one of `query`, `author`, `category`,
`kind`, or `source`.

`query` searches the existing Bleve index; it accepts Bleve query-string
syntax. Structured filters work without an index lookup. Repeat a filter for
OR; different filters and `query` combine with AND. Author is a profile URL,
category is an exact tag, and source is the **followed feed URL**, not the
publication homepage in `_source.url`. Supported kinds are `like`, `repost`,
`bookmark`, `reply`, `checkin`, `photo`, `video`, and `audio`; unknown kinds
match nothing. Kind alternatives can be repeated.

Results are newest first, with timeline-shaped `items` and `paging`. The
default page size is 20; positive `limit` values are capped at 100. Pass
`after` to fetch older results and `before` to fetch newer results; both may be
combined. Keep the original filters when following a cursor. Empty or malformed
limits, missing search criteria, empty channel, and malformed cursors return
HTTP 400; backend/index failures return HTTP 500. PostgreSQL first selects
entries belonging to the authenticated user's channels with the structured
filters and cursor bounds. Text searches check those candidates against Bleve
in batches of at most 128 IDs; stale index entries are ignored. This bounds
intermediate results and permits request cancellation, but a rare or missing
term can require scanning many owned entries and issuing multiple index
queries. The scan is not a snapshot across concurrent database/index changes.

```sh
# A query plus two alternative categories
ek query webmention home -category indieweb -category webmention -kind reply -limit 10
# Filter-only search; flags may be repeated
ek query -channel home -author https://alice.example/ -source https://alice.example/feed
# Request a later page using the printed After cursor
ek query webmention home -after 'CURSOR' -limit 10
```

The Go client provides `ItemSearchWithOptions`, which returns a `Timeline`
including cursors. The MCP `search_items` tool accepts the same filters as
arrays (`author`, `category`, `kind`, `source`), optional `query`, `channel`,
`before`, `after`, and `limit`, and returns `items` plus `paging`. `channel`
defaults to `global` in `ek query` and the MCP tool.

Search reflects the data actually stored and indexed. Newly ingested
microformats retain audio and video URLs; historical entries that were ingested
before those properties were retained must be refreshed before those kinds can
match.
