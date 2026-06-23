# rss2tg

> rss-to-telegram notification bot

[![GitHub: Release](https://img.shields.io/github/v/release/deadnews/rss2tg?logo=github&logoColor=white)](https://github.com/deadnews/rss2tg/releases/latest)
[![Docker: ghcr](https://img.shields.io/badge/docker-gray.svg?logo=docker&logoColor=white)](https://github.com/deadnews/rss2tg/pkgs/container/rss2tg)
[![CI: Main](https://img.shields.io/github/actions/workflow/status/deadnews/rss2tg/main.yml?branch=main&logo=github&logoColor=white&label=main)](https://github.com/deadnews/rss2tg)
[![CI: Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/deadnews/rss2tg/refs/heads/badges/coverage.json)](https://github.com/deadnews/rss2tg)

## Installation

```sh
docker pull ghcr.io/deadnews/rss2tg
```

## Configuration

| Variable          | Required | Default     | Description         |
| ----------------- | -------- | ----------- | ------------------- |
| `RSS2TG_TOKEN`    | yes      | —           | Telegram bot token  |
| `RSS2TG_MANAGER`  | yes      | —           | Authorized user ID  |
| `RSS2TG_INTERVAL` | no       | `10m`       | Feed check interval |
| `RSS2TG_DB_PATH`  | no       | `rss2tg.db` | Database file path  |

### Getting `TOKEN` and `MANAGER`

1. `TOKEN` — message [@BotFather](https://t.me/BotFather) → `/newbot` → copy the token.
2. `MANAGER` — open Telegram Settings → your profile → copy your numeric user ID.

## Commands

| Command                                                                         | Description                         |
| ------------------------------------------------------------------------------- | ----------------------------------- |
| `/sub <url> [link\|pw\|text] [shorts] [nolive] [exclude:w1,w2] [include:w1,w2]` | Subscribe current chat to feed      |
| `/unsub <url>`                                                                  | Unsubscribe from feed               |
| `/list`                                                                         | List subscriptions (copy-pasteable) |
| `/format <link\|pw\|text>`                                                      | Change format for all chat subs     |
| `/help`                                                                         | Show available commands             |

New subscribers receive the 3 latest entries; the rest are marked seen.

YouTube channel URLs auto-resolve to their Atom feed on `/sub`.
Shorts are filtered by default — append `shorts` to include them.
Live streams are included by default — append `nolive` to filter
them out (requires a YouTube API key).

GitHub, Gitea, and Codeberg repo URLs auto-resolve to their releases
Atom feed; pass a `/releases` or `/tags` URL to choose.

Title filters match whole words case-insensitively. Exclude wins over include.
Re-running `/sub` for an existing URL replaces its options (`/list` prints each
sub as the exact `/sub` line to copy, edit, and resend).

```sh
/sub https://example.com/feed.xml pw exclude:crypto,ai
/sub https://reddit.com/r/programming/.rss link include:go,rust
/sub https://github.com/deadnews/rss2tg
```

### Forum topics

In a forum supergroup, `/sub` from the **General** topic auto-creates a topic per
feed (the bot must be an admin with `can_manage_topics`); run it inside a topic to
subscribe there instead. Each topic keeps its own subscriptions; `/list` from
General lists them all.

## Message Formats

`link` (default) — bold title + URL:

```html
<b>Post Title</b> URL
```

`pw` — photo + title + excerpt + attribution (falls back to text if no image):

```html
<a href="URL"><b>Post Title</b></a>

Excerpt text... via <a href="FEED_URL">Feed Name</a>
```

`text` — title + feed content with links preserved:

```html
<a href="URL"><b>Post Title</b></a>

Full sanitized content with links preserved.
```
