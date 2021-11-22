# Changelog

## [Unreleased]

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
