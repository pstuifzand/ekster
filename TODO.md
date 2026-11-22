# TODO

- Generate `ek` binary with "reader" command
- Increase ease of use for people who want to try Ekster
- Hosted version??

## Code

- Remove dependency between hubIncomingHandler and memoryBackend
- Improve error handling

## Microsub protocol

Implement the protocol methods and expose them through the HTTP endpoint and client:

- `TimelineGetWithLimit` — request a timeline page size.
- `MarkUnread` — mark entries unread.
- `MarkReadThrough` — mark an entry and earlier entries read using `last_read_entry`.
- `TimelineRemove` — remove entries from a channel.
- `ItemSearchWithOptions` — support content search filters (`author`, `category`, `kind`, `source`), paging and `limit`; also accept GET requests.
- `MuteGetList`, `MuteURL`, `UnmuteURL` — list, mute and unmute users.
- `BlockGetList`, `BlockURL`, `UnblockURL` — list, block and unblock users.

The remaining interface methods currently return `ErrUnsupported`.

## DONE

- `ChannelsOrder` — reorder channels through Microsub, `ek`, and MCP.
- Compile templates into binary (with "embed"?)
- Per user backends
