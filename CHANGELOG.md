# Changelog

## [Unreleased]

### Added

- Use the fetched HTML page's title and URL as the fallback feed author when no author card is available.

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
