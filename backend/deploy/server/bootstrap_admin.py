"""Create the LAN owner's account once, without sending verification email."""
import json
import os
from pathlib import Path
import secrets
import subprocess

access = Path('/opt/shortlong/admin-access.json')
if access.exists():
    raise SystemExit('Existing administrator credentials retained.')
email = 'server-admin@local.dev'
password = secrets.token_urlsafe(24)
sql = """INSERT INTO users(email,password_hash,public_id,display_name,plan,subscription_expires_at,email_verified,is_admin,primary_exchange)
VALUES ('%s',crypt('%s',gen_salt('bf',10)),100001,'Server Admin','pro',now()+interval '10 years',true,true,'bybit');""" % (email, password)
subprocess.run(['docker','exec','-i','crypto_postgres','psql','-U','postgres','-d','alerts','-v','ON_ERROR_STOP=1'], input=sql, text=True, check=True, stdout=subprocess.DEVNULL)
os.umask(0o077)
with access.open('x') as f:
    json.dump({'url':'http://192.168.0.124','email':email,'password':password},f)
print('LAN administrator created; credentials saved in /opt/shortlong/admin-access.json (0600).')
