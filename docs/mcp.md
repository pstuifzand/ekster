# Ek MCP server

`ek mcp` starts a standard MCP server over stdio for Ek's one configured
Microsub account. Run `ek connect <website-url>` first. The server uses the
existing saved token and endpoint; it does not print protocol data or tokens to
logs/stdout. Diagnostics go to stderr.

The command uses the official Go MCP SDK v0.8.0, compatible with this
repository's Go 1.23 baseline. Tools use inferred, validated JSON input schemas,
explicit typed output schemas, and structured tool results (`structuredContent`).

## Read-only tools

- `list_channels`: channel UIDs, names, unread counts.
- `get_timeline`: `channel` plus optional `before`/`after` cursors; returns items
  and paging cursors.
- `search_feeds`: search feed sources by query.
- `search_items`: search timeline items, optionally in a channel; defaults to
  the global timeline.

The read-only status and open-world hints are MCP annotations, not a security
boundary. The MVP intentionally does not expose follow, channel mutation, or
mark-read tools.

## Example MCP client configuration

For an MCP client that accepts a `mcpServers` configuration:

```json
{
  "mcpServers": {
    "ek-microsub": {
      "command": "/absolute/path/to/ek",
      "args": ["mcp"]
    }
  }
}
```

The client must launch Ek as a subprocess and connect using stdio transport.
