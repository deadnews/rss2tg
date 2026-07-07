# rss2tg

> rss-to-telegram notification bot

[![GitHub: Release](https://img.shields.io/github/v/release/deadnews/rss2tg?logo=github&logoColor=white)](https://github.com/deadnews/rss2tg/releases/latest)
[![Docker: ghcr](https://img.shields.io/badge/docker-gray.svg?logo=docker&logoColor=white)](https://github.com/deadnews/rss2tg/pkgs/container/rss2tg)
[![CI: Main](https://img.shields.io/github/actions/workflow/status/deadnews/rss2tg/main.yml?branch=main&logo=github&logoColor=white&label=main)](https://github.com/deadnews/rss2tg)
[![CI: Coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/deadnews/rss2tg/refs/heads/badges/coverage.json)](https://github.com/deadnews/rss2tg)

**[Installation](#installation)** • **[Configuration](#configuration)** • **[Commands](#commands)** • **[Feeds](#feeds)** • **[Message Formats](#message-formats)** • **[Supergroup topics](#supergroup-topics)**

## Installation

```sh
docker pull ghcr.io/deadnews/rss2tg
```

See [`compose.dev.yml`](compose.dev.yml) for a Compose reference.

## Configuration

| Variable             | Required | Default     | Description          |
| -------------------- | -------- | ----------- | -------------------- |
| `RSS2TG_TOKEN`       | yes      | —           | Telegram bot token   |
| `RSS2TG_MANAGER`     | yes      | —           | Authorized user ID   |
| `RSS2TG_INTERVAL`    | no       | `10m`       | Feed check interval  |
| `RSS2TG_DB_PATH`     | no       | `rss2tg.db` | Database file path   |
| `RSS2TG_YOUTUBE_KEY` | no       | —           | YouTube Data API key |

### Getting `TOKEN` and `MANAGER`

1. `TOKEN` — message [@BotFather](https://t.me/BotFather) → `/newbot` → copy the token.
2. `MANAGER` — open Telegram Settings → your profile → copy your numeric user ID.

## Commands

| Command                                                                         | Description                     |
| ------------------------------------------------------------------------------- | ------------------------------- |
| `/sub <url> [link\|pw\|text] [shorts] [nolive] [exclude:w1,w2] [include:w1,w2]` | Subscribe current chat to feed  |
| `/unsub <url>`                                                                  | Unsubscribe from feed           |
| `/list`                                                                         | List subscriptions              |
| `/format <link\|pw\|text>`                                                      | Change format for all chat subs |
| `/help`                                                                         | Show available commands         |

```text
/sub https://example.com/feed.xml pw exclude:crypto,ai
/sub https://reddit.com/r/programming/.rss link include:go,rust
/sub https://github.com/deadnews/rss2tg
```

- New subscribers receive the 3 latest entries; the rest are marked seen.
- `/list` prints each sub as the exact `/sub` line to copy, edit, and resend.
- In a direct message, `/list` spans every chat, grouped by chat title.
- Re-running `/sub` for an already-subscribed URL replaces its options.
- Title filters match whole words case-insensitively; exclude wins over include.

## Feeds

- YouTube channel URLs auto-resolve to their Atom feed.
  - Shorts are filtered by default — append `shorts` to include them.
  - Live streams are included by default — append `nolive` to filter them out.
  - Without `RSS2TG_YOUTUBE_KEY`, messages are sent without
    duration and live metadata; `nolive` has no effect.
- GitHub repo URLs auto-resolve to their releases Atom feed.

## Message Formats

`link` (default) — bold title + URL:

```text
<b>Post Title</b>
URL
```

`text` — title + feed content with links preserved:

```text
<a href="URL"><b>Post Title</b></a>

Full sanitized content with links preserved.
```

`pw` — photo + title + excerpt + attribution (falls back to text if no image):

```text
<a href="URL"><b>Post Title</b></a>

Excerpt text...

via <a href="FEED_URL">Feed Name</a>
```

## Supergroup topics

- `/sub` from the **General** topic auto-creates a topic per feed.
  - The bot must be an admin with `can_manage_topics`.
- `/sub` inside a topic subscribes there.
- `/list` from **General** lists every topic's subscriptions.
- `/list` inside a topic lists just that topic's.
