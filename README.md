# Lightning Checker

Username availability checker for Discord, Telegram, Instagram and TikTok. Written in Go, standard library only.

## Build
1. Install Go from https://go.dev/dl/
2. Double-click `build.bat` to get `LightningChecker.exe` with `icon.ico` embedded.
   To use another icon, run `build.bat myicon.ico`.
3. Best viewed in Windows Terminal (default on Windows 11). The old console works too.

## Files
- `users.txt`: one username per line. A leading `@` is removed and duplicates are skipped.
  Use menu option 5 to pick another list (you can drag a file into the window).
- `proxies.txt` (optional): `ip:port`, `ip:port:user:pass`, `user:pass@ip:port` or `socks5://...` (duplicates removed)
- `hits/`: a new `<mode>_<date>.txt` for each run. Menu option H opens the folder.
- `users_left.txt`: written when you stop a run early, so you can continue from there.
- `config.json`: settings and saved logins. Don't share it.

## Features
- Animated intro and a logo shimmer in the menu. Turn both off with option 8.
- Live footer with a progress bar, %, ETA, hits, checked, left, speed and retries.
- Optional Discord webhook that posts every hit (Saved logins → Webhook). It's tested when you save it.
- Clicking in the console no longer freezes a run (QuickEdit is paused while checking).
- Ctrl+C stops cleanly: open requests finish, then the summary is shown and unchecked names are saved.

## Speed and rate limits
- **Proxy scheduler:** each proxy has its own state. Workers always take the next proxy that is free, not cooling down and not rate limited, so no request waits on a limited proxy while another is free.
- **Exact limits (Discord):** reads `X-RateLimit-Limit / Remaining / Reset-After` on every reply and stops using a proxy *before* it runs out. In a simulation this ran at 100% of the theoretical maximum with almost no 429s.
- **Account limits:** with a Discord token the limit belongs to the account, so all workers share one account-wide limit. The footer shows `LIMITED 2.3s` while waiting.
- **Retry-After:** used exactly on every platform when a site sends it.
- **Adaptive (Telegram, Instagram, TikTok):** with no limit headers, each proxy learns its own safe pace. It measures how fast it got before a 429, slows to just under that rate, then slowly speeds back up. Repeated limits back off exponentially.
- **Auto switching:** dead proxies get exponentially longer cooldowns (up to 2 min), so they're tried less and less while working ones take over. Being rate limited doesn't count as a failed check, only connection errors do.
- **Proxy test:** on start (or from menu P), all proxies are tested at 200 at a time. Dead ones are dropped and the fastest are used first. From menu P you can save the cleaned list (the old one goes to `proxies_backup.txt`).
- **Workers = Auto:** proxies × per-proxy requests (default 2), capped at 2000. With no proxies it uses 5.

## How each mode checks
| Mode | Method | Login |
|---|---|---|
| Discord | register-page check, or `pomelo-attempt` if a token is set | optional token |
| Telegram | t.me page, then fragment.com, so names held on Fragment don't count as hits | none |
| Instagram | `web_profile_info` | optional sessionid |
| TikTok | profile page `statusCode` (10202 means not found) | none |

## Notes
- If a site changes its responses, those checks are counted as **retries**, not hits. All the check code is in `platforms.go`.
- Instagram and TikTok also show banned or deactivated names as "not found", so some hits may not be claimable.
- Automating user tokens or sessions breaks Discord and Instagram ToS. Use alt accounts.
