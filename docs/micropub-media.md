# The Micropub media endpoint

A client that wants to attach a photo uploads it to `/media` first and puts the
returned URL in the Micropub request that creates the post. That is what the
Media Endpoint section of the Micropub spec asks for, and it is the only way to
attach a file to a JSON post.

## Discovery

`GET /micropub?q=config` carries the endpoint:

```json
{
  "post-types": [{"type": "entry", "name": "Note"}],
  "visibility": ["private"],
  "syndicate-to": [],
  "media-endpoint": "https://ekster.example/media"
}
```

The value is always the site's own origin plus `/media`, built from `-baseurl`.
A client must not assume the media endpoint shares a host with anything else, so
the full URL is what is advertised.

## Upload

```http
POST /media HTTP/1.1
Authorization: Bearer <token>
Content-Type: multipart/form-data; boundary=...

--...
Content-Disposition: form-data; name="file"; filename="cat.png"
Content-Type: image/png

<bytes>
```

- Authentication is the Micropub authentication: a source token or IndieAuth
  token in the `Authorization` header, or the same token as an `access_token`
  form field. The media endpoint accepts exactly the tokens the Micropub
  endpoint accepts.
- The body must be `multipart/form-data` with one part named `file`. Other parts
  are ignored.
- Up to 10 MiB. `413` beyond that, `400` for an empty file or a missing part.
- The response is `201 Created` with `Location: <site>/media/<uid>` and no body.
  Errors are JSON `{"error": "..."}`, as the rest of Micropub.

## Reading

`GET /media/<uid>` is public. That is deliberate: a post's `<img src>` cannot
send a token, and the reader shows photos from the item's `photo` property. The
uid is 16 random bytes as hex — the URL is the capability, and it is not listed
anywhere.

- `Content-Type` is what sniffing the bytes says, not what the client claimed.
  The one exception is `application/octet-stream`, where audio and video keep
  the declared type because sniffer support for them is poor.
- `Cache-Control: public, max-age=31536000, immutable`, with an `ETag` of the
  uid; `If-None-Match` answers `304`.
- `X-Content-Type-Options: nosniff` on every response.
- `Content-Disposition: inline` only for `image/*`, `audio/*` and `video/*`
  (SVG excluded — it is a scriptable document). Everything else is served as an
  attachment, so an uploaded HTML or SVG file cannot run from Ekster's origin.
- `HEAD` answers the headers with no body.

## Storage

The bytes live in the `media` table (`000023_create_media`), keyed by uid and
pointing at the channel that uploaded them. One database backup therefore covers
posts and the files they mention, and deleting a channel deletes its media with
it (`ON DELETE CASCADE`).

There is no cleanup job: an upload that never ends up in a post stays until the
channel is deleted. Add expiry (`created_at` is stored) if that ever matters.

## Verifying it by hand

Against a local server with a source token:

```sh
TOKEN=...   # the source token from /settings/sources
curl -s -D - -o /dev/null -X POST http://localhost:8089/media \
  -H "Authorization: Bearer $TOKEN" -F "file=@photo.png;type=image/png"
# 201, Location: http://localhost:8089/media/<32 hex>

curl -s -D - -o /dev/null http://localhost:8089/media/<32 hex>
# 200, image/png, inline, nosniff, immutable

curl -s -X POST http://localhost:8089/micropub \
  -H "Authorization: Bearer $TOKEN" \
  --data-urlencode "h=entry" --data-urlencode "content=photo" \
  --data-urlencode "photo=http://localhost:8089/media/<32 hex>"
```

The item page at `/i/<uid>` shows the photos of an item, and the reader shows
them from the same `photo` property.

Two traps when doing this locally:

- An item create needs Redis (`generateItemID` increments a key); a media
  upload does not. A 500 with "connection refused" on `:6390` on the Micropub
  call means the Redis container is missing, not that media storage broke.
- Verify the binary the port serves (`sha256sum /proc/<pid>/exe`) before
  believing any of it; see `references/item-content-rendering.md` in the
  `ekster-microsub-development` skill.
