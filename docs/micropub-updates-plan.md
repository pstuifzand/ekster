# Plan: Micropub updates for Ekster

## Goal

Implement the standard Micropub `action=update` operation so an authorized Micropub client can update an item it previously created, using the URL returned in the create response. This supports general Micropub clients, including P83's Webmention notification integration, without introducing an Ekster-specific update API.

This is an update-by-existing-URL feature, not an idempotent-create guarantee. If a create succeeds but its response is lost before the client records the returned URL, Micropub alone does not ensure a retry will avoid creating a duplicate.

## Current behavior

- `POST /micropub` resolves a source token to its channel, parses a new item, generates a new item ID, and adds the item.
- A successful create returns `201 Created` and the item's URL in `Location`.
- `GET /micropub?q=config` and `GET /micropub?q=source` are supported.
- The Micropub handler does not currently process `action=update`.
- Timeline storage interfaces primarily expose `AddItem`; update-by-ID needs explicit backend support and consistent behavior across configured storage implementations.

## Design

### Request handling

1. Continue resolving the bearer/source token before performing any operation. The source token scopes the operation to its configured channel.
2. Parse the standard Micropub update request: `action=update`, `url=<existing item URL>`, and the standard `replace[]`, `add[]`, and `delete[]` property instructions.
3. Resolve the URL to an Ekster item ID and verify that the item exists in the token's channel. Reject malformed URLs, missing items, and items outside the authorized channel. Do not permit clients to choose or move an item into another channel.
4. Apply supported property operations to the existing item, then persist it under the same item ID and URL. Do not route update requests through the create path or generate a new ID.
5. Return a successful Micropub response while preserving the original item URL. Return appropriate client errors for invalid requests, unsupported operations, and inaccessible items.

### Property semantics

- Implement Micropub's standard replace, add, and delete semantics for properties Ekster supports.
- Define and test behavior for scalar and multi-valued properties, including content, name, category, and the supported microformat properties.
- Treat an absent update operation distinctly from an explicit delete. Do not silently create unsupported or arbitrary fields.
- Check the current Micropub specification before implementation for exact request encoding and property-operation semantics; support its standard form-encoded update representation.

### Storage and events

1. Add an update-by-item-ID capability to the storage/backend interfaces used by Micropub.
2. Implement it for each active storage backend. Preserve the stable item ID, channel membership, publication identity, and read state unless the requested update explicitly changes an allowed property.
3. A content update is not a new item: avoid duplicate timeline entries and avoid falsely incrementing unread counts.
4. If the reader should refresh an already-visible item, add or use an appropriate update event rather than emitting a misleading create event. Document how clients that do not support such events observe updated content on their next timeline fetch.
5. Verify database uniqueness/upsert behavior and Redis persistence semantics; do not assume all backends behave like the PostgreSQL implementation.

### Micropub discovery and config

- Keep advertising the existing `/micropub` endpoint as it is today.
- Review `q=config` and advertise update support only in the manner prescribed by the Micropub specification, if a capability declaration is required. Do not add non-standard metadata solely for P83.

## Testing

- A client can create an item, retain its `Location`, and update the item through the standard request format.
- Update preserves the item's URL and ID and changes only the requested properties.
- Add/delete/replace semantics work for scalar and multi-valued properties.
- A token can update an item only in its scoped channel; invalid, missing, or cross-channel URLs are rejected.
- An update does not create another item, mark an already-read item unread, or falsely increase the channel's unread count.
- Reader event behavior is covered if update events are implemented.
- Existing create, query, source-token, and legacy token behavior remains unchanged.
- Run the Go test suite and applicable database/storage integration tests.

## Out of scope

- A custom idempotency header or request field.
- Exactly-once create delivery when the response to the first create is lost.
- Automatically deduplicating creates based on notification content or caller-supplied `uid` without a separately specified and reliable standard-compatible contract.
- P83-specific Webmention logic in Ekster.

## Rollout

1. Implement the protocol parsing, authorization, storage update path, and tests.
2. Deploy Ekster and verify standard Micropub create/update behavior with a test client.
3. Create a dedicated source token scoped to the Notifications channel for P83.
4. P83 can then retain each create response URL and use standard Micropub updates for subsequent changes to that notification.
