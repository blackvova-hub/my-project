# Telegram server admin bot

This bot is a private, read-only operational console for server01. It can read
container status and logs through Docker's Unix socket, so it must only allow
chats explicitly linked by the owner. It cannot execute arbitrary shell
commands, change services, or reveal environment variables/secrets.

## First start on server01

1. Create a **new** bot through `@BotFather` and copy its HTTP API token.
2. Generate a separate, long random bootstrap secret, for example:

   ```sh
   openssl rand -hex 24
   ```

3. On the server, edit `/opt/shortlong/backend/.env` (mode 0600) and add:

   ```dotenv
   ADMIN_BOT_TOKEN='123456:token-from-BotFather'
   ADMIN_BOT_BOOTSTRAP_SECRET='long-random-secret'
   ```

4. Start only the bot profile:

   ```sh
   cd /opt/shortlong/backend
   sudo ./deploy/server/compose.sh --profile telegram-admin up -d --build telegram_admin_bot
   ```

5. Open a private chat with the new bot and send:

   ```text
   /claim long-random-secret
   ```

The first successful claim permanently links that Telegram chat. The bootstrap
secret is then refused, so it cannot be reused to add another user. Use `/id`
in a second private chat and `/grant <numeric-chat-id>` from the first chat to
add another administrator.

## Commands

- `/status` — current server, CPU/RAM, temperatures, disks, containers and
  archive summary.
- `/host` — the latest full host-health snapshot.
- `/archive` — Bybit/Binance 1-minute archive progress and source gaps.
- `/containers` — container state, health, restart and OOM status.
- `/temperature` and `/disk` — live host temperatures and filesystem space.
- `/errors` — recent warnings/errors across the relevant services.
- `/logs archive|backend|clickhouse|postgres|redis|news|worker` — last log
  lines for one approved service.
- `/id`, `/grant <chat-id>`, `/revoke <chat-id>`, `/help`.

The authorization state is stored in the Docker volume `telegram_admin_state`;
it survives container recreation. Never send the BotFather token or bootstrap
secret to anyone. If the first chat is lost, stop the bot and deliberately
remove/reset only that volume after confirming the target.

## Connectivity on server01

The server Compose overlay assigns the admin bot `172.31.254.4` on the existing
VPN egress bridge and uses explicit DNS resolvers. The routing timer maintains
the owned source rule at priority `32122` through the existing `awg0` table;
application traffic, the host default route and authorization storage retain
their existing configuration. If Telegram becomes unreachable, the healthcheck
now fails its bounded `getMe` request instead of reporting Docker access alone
as healthy. Transport diagnostics redact the bot token.
