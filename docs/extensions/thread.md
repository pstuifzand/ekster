# Microsub extension: thread

Status: draft, experimental. This document extends the published Microsub
specification (https://indieweb.org/Microsub-spec) and changes nothing in it.

A client that does not implement this extension is unaffected: it never sends
`method=thread`, and the plain timeline keeps answering exactly as before.

## Motivation

Microsub carries the post types that make a conversation visible — `in-reply-to`,
`like-of`, `repost-of`, `bookmark-of` — but no way to ask for the conversation
those posts belong to. A reader can show that someone replied; it cannot show
what they replied to, or who else replied.

The set of posts a Microsub server can see is its subscriptions. The set of
posts in a conversation is not. Those two sets intersect, and the difference is
exactly what a client cannot compute for itself: the participants of a
conversation are, almost by definition, posts the user does not follow.

## Request

The extension reuses the existing `timeline` action, so no new action is
introduced:

    GET /microsub?action=timeline&method=thread&entry={_id}
    GET /microsub?action=timeline&method=thread&url={permalink}

    action=timeline     MUST be timeline
    method=thread       MUST be thread
    entry={_id}         an entry in one of the user's channels
    url={url}           the permalink of a post
    channel={uid}       optional: scopes read state, disambiguates entry
    depth={n}           optional: how far up the in-reply-to chain to walk

`entry` or `url` MUST be supplied, and not both.

`depth` SHOULD default to 10 and MUST be capped by the server. Because a thread
is capped rather than paged, the cap is the only bound on how much work one
request can cause; a server SHOULD document the cap it enforces (Ekster: 20).

`channel` narrows `entry` to one channel. It is required only when the same
entry may appear in more than one of the user's channels, because read state is
per channel and the answer would otherwise be ambiguous.

A server MAY accept the same parameters as a form-encoded POST body, for
clients that use POST for every action. The parameters and their meaning are
identical.

## Response

```json
{
  "items": [
    {
      "type": "entry",
      "_id": "a1c9",
      "_is_read": true,
      "url": "https://ann.example/2026/11/on-threads",
      "published": "2026-11-14T09:12:00+00:00",
      "in-reply-to": "https://dan.example/2026/11/earlier",
      "content": { "html": "<p>The post.</p>" },
      "author": { "type": "card", "name": "Ann", "url": "https://ann.example/" }
    },
    {
      "type": "entry",
      "url": "https://bob.example/re-threads",
      "published": "2026-11-14T11:40:00+00:00",
      "in-reply-to": "https://ann.example/2026/11/on-threads",
      "content": { "html": "<p>A reply.</p>" },
      "author": { "type": "card", "name": "Bob", "url": "https://bob.example/" }
    }
  ],
  "thread": {
    "root": "https://ann.example/2026/11/on-threads",
    "partial": true,
    "missing": 1
  }
}
```

`items` is in conversation order: the root first, then every known reply ordered
by `published` ascending. Each item keeps its `in-reply-to`, so a client
reconstructs the tree without a second request and replies to replies are not
flattened into an unreadable list.

`thread.root` is the permalink of the topmost ancestor the server could resolve.

`thread.partial` is `true` when the server knows of ancestors or replies it
could not return. `thread.missing` is how many ancestors are known but
unresolvable: the count behind "earlier posts on the original site".

There is no `paging` object. A thread is a conversation, not a page. Servers
MUST NOT paginate it, and MUST cap it instead.

## Responses and errors

| Situation                                   | Status |
|---------------------------------------------|--------|
| found                                       | 200    |
| neither `entry` nor `url`, or both           | 400    |
| `entry` is not in the requesting user's channels | 404 |
| `url` cannot be resolved to a post           | 404    |
| token lacks read access                      | 403    |
| unknown method at the server (no extension)  | 400    |

## Rules

**Ancestors may be fetched; replies may not.** A server MUST NOT crawl the web
looking for replies. Replies are limited to entries the server has already
indexed. Walking up the `in-reply-to` chain to fetch a missing ancestor is
allowed, bounded by `depth`.

**Fetched ancestors are not subscriptions.** An entry fetched for a thread MUST
NOT be added to a channel, MUST NOT affect unread counts, and MUST NOT be
announced over `events`.

**State belongs to the channel.** An entry in one of the user's channels MAY
carry `_id` and `_is_read`. An entry the server fetched from outside the
channels MUST NOT carry `_id`, and MUST NOT carry `_is_read`: there is no
channel in which it could be marked read, so a read state on it would be a fact
the server invented.

**Another user's entries are never visible.** Scoping is by token, not by
`_id`. An entry owned by another user's channel MUST be indistinguishable from
one that does not exist.

**Bound the work.** Servers MUST enforce a depth cap, a maximum number of
items, and a total time budget for the request, and SHOULD cache a resolved
thread. A thread request MUST NOT be able to make the server issue an unbounded
number of outbound requests.

**Results are partial by contract.** Clients MUST NOT present a thread as the
complete conversation. `thread.partial` exists so the interface can say so
instead of implying it.

## Security considerations

Thread resolution follows links that a feed chose, not links the user chose.
`follow` and `preview` are the user's business; an `in-reply-to` value in a
subscribed entry is not, and a server that fetches it unguarded becomes a proxy
for whatever the feed points at.

A server MUST NOT resolve ancestors at addresses a client could not reach
itself: loopback, link-local (including cloud metadata at 169.254.169.254),
private ranges, carrier-grade NAT, unique-local IPv6, and the server's own
interfaces. The check MUST be applied to the *resolved address*, not the
hostname, or DNS rebinding and names that resolve to loopback defeat it. It
MUST also apply to every redirect hop.

Ancestors are attacker-controlled content. A server MUST sanitize their HTML
like any feed item, MUST cap their size, and MUST NOT let them set `_id`,
`_is_read` or `_source`.

## Compatibility

There is no capability declaration in Microsub, so a client discovers this
extension by asking: send the request, and treat `400` as "this server does not
implement thread" and fall back to showing the entry alone. That is the same
probe-and-degrade pattern clients already need for optional actions.

A server that implements the extension MUST keep `action=timeline` without
`method=thread` answering as before.

## Reference implementation notes

Ekster (`github.com/pstuifzand/ekster`) implements this as
`action=timeline&method=thread`, with:

- two expression indexes on the item store: a btree over `data->>'url'` for the
  permalink lookup and a GIN over `data->'in-reply-to'` for the replies. An
  index over the whole document does not answer the second query, because the
  operator's left side is a nested value — measured, not assumed.
- ancestors cached as documents, not as item rows: storing a fetched ancestor
  as a row would collide with the item's uid the first time the user actually
  follows that feed, and leave it outside every timeline.
- the public-address check in `net.Dialer.Control`, which sees the resolved
  address and covers every redirect hop by construction.
