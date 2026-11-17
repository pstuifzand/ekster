# Changelog

## [Unreleased]

### Added

- `ek thread`, and the `action=timeline&method=thread` extension behind it: the conversation around an entry, root first and the known replies oldest first, with `thread.partial` and `thread.missing` saying what the answer does not contain. Ancestors are walked up `in-reply-to` and fetched when missing; replies come only from what is already indexed. Documented in `docs/extensions/thread.md`.
- Following a bare search term in the feed follower: the search now offers the matching Hacker News stories and comments as ordinary feeds (`hnrss.org`), so a subject like "postgres" can be followed, previewed and unfollowed like any other feed.
- Use the fetched HTML page's title and URL as the fallback feed author when no author card is available.
- A fetched page that publishes no microformats becomes a card from its own metadata (`og:title`, `og:description`, `og:image`, `<title>`), so a thread shows the post an entry replies to even on Squarespace, Medium or a plain blog.

### Changed

- A search feed polls on a fixed sixteen-minute rung instead of the tier ladder, which would let a stream that never goes quiet climb to hours and then days, and it gets a twenty-second fetch budget because `hnrss.org` renders the query on demand (11s cold, 0.5s cached) rather than serving a static file.

### Fixed

- Read the entry's own author from `dc:creator` (RSS 2.0 and RSS 1.0) and atom `<author><name>`; every entry of an RSS or atom feed was stored with the feed's title as its author, so a search feed showed twenty entries all written by the feed itself.

## [0.10.0] - 2026-11-11

### Added

- Added `ek reader`, an embedded Ekstereye reader with a loopback-only local server and Ek-managed IndieAuth connection.
- Added `ek mcp`, a stdio MCP server exposing typed, structured, read-only Microsub tools for channels, timelines, and search.
- Added documentation for the MCP integration and the planned local `ek serve` mode.

### Changed

- Updated Ek CLI version to 0.10.0.

## [1.0.0-rc.1] - 2021-11-20

### Added

- Postgresql support for channels, feeds, items and subscriptions.
- Support "source" in items and feeds

### Changed

- Default channel backend is postgresql

### Fixed

- All `staticcheck` problems are fixed.
- Dependency between memorybackend and hubbackend removed and simplified.

### Deprecated

- All Redis timeline types are deprecated and will be removed in a later version.

[Unreleased]: https://git.p83.nl/peter/ekster/compare/1.0.0-rc.1...master
[1.0.0-rc.1]: https://git.p83.nl/peter/ekster/src/tag/1.0.0-rc.1
