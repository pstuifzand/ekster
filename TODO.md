# TODO

- Generate `ek` binary with "reader" command
- Increase ease of use for people who want to try Ekster
- Hosted version??

## Code

- Remove dependency between hubIncomingHandler and memoryBackend
- Improve error handling

## Microsub protocol

Implement the protocol methods and expose them through the HTTP endpoint and client:

- `ItemSearchWithOptions` — support content search filters (`author`, `category`, `kind`, `source`), paging and `limit`; also accept GET requests.
- `MuteGetList`, `MuteURL`, `UnmuteURL` — list, mute and unmute users.
- `BlockGetList`, `BlockURL`, `UnblockURL` — list, block and unblock users.

The remaining interface methods currently return `ErrUnsupported`.

## DONE

- `TimelineRemove` — remove entries from a channel through Microsub, `ek`, and MCP.

- `MarkReadThrough` — mark an entry and earlier entries read using `last_read_entry`.

- `MarkUnread` — mark entries unread through Microsub, `ek`, and MCP.

- `TimelineGetWithLimit` — request a timeline page size through Microsub, `ek`, and MCP.

- `ChannelsOrder` — reorder channels through Microsub, `ek`, and MCP.
- Compile templates into binary (with "embed"?)
- Per user backends
