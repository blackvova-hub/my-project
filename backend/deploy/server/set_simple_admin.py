"""Set the owner-requested simple credentials on the account we bootstrapped."""
import json
import os
import getpass
import subprocess
from pathlib import Path

path = Path('/opt/shortlong/admin-access.json')
access = json.loads(path.read_text())
assert access['email'] in ('server-admin@local.dev', 'admin@local.dev')
password = os.environ.get('ADMIN_INITIAL_PASSWORD') or getpass.getpass('New LAN admin password: ')
if len(password) < 8:
    raise SystemExit('Use at least eight characters')
literal = password.replace("'", "''")
sql = """UPDATE users SET email='admin@local.dev',
password_hash=crypt('%s',gen_salt('bf',10)),
email_verified=true,two_fa_enabled=false,is_admin=true,
plan='pro',subscription_expires_at=now()+interval '10 years'
WHERE email='%s';""" % (literal, access['email'])
subprocess.run(['docker','exec','-i','crypto_postgres','psql','-U','postgres','-d','alerts','-v','ON_ERROR_STOP=1'],input=sql,text=True,check=True)
access.update(email='admin@local.dev',password=password)
path.write_text(json.dumps(access))
path.chmod(0o600)
