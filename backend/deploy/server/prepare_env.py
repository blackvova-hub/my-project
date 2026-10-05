"""Run once on server01. Preserve provider credentials without logging values."""
import os
import secrets
from pathlib import Path

root = Path('/opt/shortlong/backend')
target = root / '.env'
if target.exists():
    raise SystemExit('Existing server .env retained; edit deliberately instead of regenerating secrets')
seed = root / '.env.seed'
values = {}
if seed.exists():
    for line in seed.read_text(encoding='utf-8-sig').splitlines():
        if '=' in line and not line.lstrip().startswith('#'):
            key, value = line.split('=', 1)
            values[key.strip()] = value.strip().strip('\"').strip("'")
# Only integration configuration is carried over; host/database credentials are new.
allowed = ('SMTP_', 'OPENAI_', 'TIMEWEB_', 'CRYPTOPANIC_', 'SOSOVALUE_', 'ETHERSCAN_', 'TRONGRID_', 'TELEGRAM_BOT_', 'TELEGRAM_WEBHOOK_')
values = {k: v for k, v in values.items() if k.startswith(allowed) and '\n' not in v}
values.update({
    'POSTGRES_PASSWORD': secrets.token_hex(24),
    'REDIS_PASSWORD': secrets.token_hex(24),
    'CLICKHOUSE_PASSWORD': secrets.token_hex(24),
    'QDRANT_API_KEY': secrets.token_hex(24),
    'RESET_TOKEN_SECRET': secrets.token_hex(32),
    'FRONTEND_URL': 'http://192.168.0.124',
    'APP_ENV': 'local',
    'TURNSTILE_ENABLED': 'false',
    'MARKET_HISTORY_RPS': '8',
    'MARKET_HISTORY_MONTHS': '12',
})
os.umask(0o077)
with target.open('x', encoding='utf-8') as f:
    for key, value in sorted(values.items()):
        # Compose single-quoted values avoid dollar interpolation in provider credentials.
        f.write(key + "='" + value.replace("'", "\\'") + "'\n")
(root / 'telegram/news-publisher/.env').touch(mode=0o600, exist_ok=True)
if seed.exists():
    seed.unlink()
print('Server secrets generated; integration settings copied without printing credentials.')
