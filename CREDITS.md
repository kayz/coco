# Credits

## Origin

coco is derived from [lingti-bot](https://github.com/pltanton/lingti-bot), an open-source personal AI assistant built in Go.

**Fork date**: 2026-02-25

**Original author**: pltanton and contributors

**License**: MIT License (see below)

---

## What we inherited from lingti-bot

The following capabilities were present in the codebase at the time of the fork:

- Multi-provider AI integration (Claude, DeepSeek, Kimi, Qwen, OpenAI, and 15+ compatible providers)
- Platform integrations: WeCom (企业微信), Feishu (飞书), DingTalk (钉钉), Telegram, Discord, Slack, LINE, Matrix, Mattermost, Nextcloud, Signal, Twitch, NOSTR, Zalo, WhatsApp, Google Chat, Teams
- MCP (Model Context Protocol) server — stdio and SSE modes
- Tool system: file operations, shell execution, system info, network, browser automation (go-rod)
- Voice: Whisper STT, ElevenLabs TTS
- Cron scheduling (robfig/cron, second-level precision)
- RAG long-term memory (chromem-go, vector search)
- User preference auto-learning
- SQLite persistence (WAL mode)
- WebSocket gateway (:18789)
- Cloud relay client (keeper.kayz.com)
- Skills system (JSON definitions, 8 built-in skills)
- `--onboard` setup wizard framework
- Single binary (Go compiled)

---

## What coco adds

Starting from 2026-02-25, coco introduces a new direction:

- **Dual-agent architecture**: Keeper (public server) + coco (local machine)
- **Self-hosted Keeper**: replaces dependency on keeper.kayz.com for WeCom relay
- **`coco keeper` subcommand**: public-facing WebSocket + WeCom webhook server
- See `VISION.md` for the full roadmap

---

## Original MIT License

```
MIT License

Copyright (c) pltanton and lingti-bot contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
