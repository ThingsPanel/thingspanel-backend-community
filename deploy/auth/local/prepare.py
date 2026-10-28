"""Prepare a LOCAL ONLY Casdoor fixture. Never reads production credentials.

Usage: python prepare.py <new-private-directory>
Requires Python cryptography. Generated CA is valid for seven days and is NOT
installed into the operating system trust store.
"""
import datetime
import ipaddress
import json
from pathlib import Path
import secrets
import sys
import urllib.request

from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.x509.oid import NameOID

directory = Path(sys.argv[1]).resolve()
if directory.exists() and any(directory.iterdir()):
    raise SystemExit("Use a new empty private directory; existing fixtures are never overwritten")
directory.mkdir(parents=True, exist_ok=True)
(directory / "data").mkdir()
with urllib.request.urlopen("https://raw.githubusercontent.com/casdoor/casdoor/v4.23.0/init_data.json.template", timeout=30) as response:
    template = json.load(response)
organization = template["organizations"][0]
organization.update(owner="admin", name="tp-local", displayName="ThingsPanel local identity test", passwordType="plain", defaultApplication="tp-local")
application = template["applications"][0]
application.update(owner="admin", name="tp-local", displayName="ThingsPanel Local", organization="tp-local", clientId="tp-local-client", clientSecret=secrets.token_urlsafe(32), cert="cert-built-in", enableSignUp=False, redirectUris=["https://localhost:24444/api/v1/auth/external/callback"], grantTypes=["authorization_code"], tokenFormat="JWT", signinMethods=[dict(name="Password", displayName="Password", rule="All")])
user = template["users"][0]
user.update(owner="tp-local", name="tester", displayName="Local tester", password="Local-test-2026!", email="tester@example.invalid", isAdmin=False)
(directory / "init_data.json").write_text(json.dumps(dict(organizations=[organization], applications=[application], users=[user])), encoding="utf-8")
(directory / "app.conf").write_text("\n".join([
    "appname = casdoor", "httpport = 8000", "runmode = prod", "driverName = sqlite",
    "dataSourceName = file:/data/casdoor.db?cache=shared", "dbName = casdoor", "showSql = false",
    "origin = https://localhost:24443", "originFrontend = https://localhost:24443",
    "copyrequestbody = false", "logPostOnly = true", "initDataNewOnly = true",
    "initDataFile = /init_data.json", "initAdminPassword = " + secrets.token_urlsafe(32)
]), encoding="utf-8")
key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, "ThingsPanel local test CA")])
now = datetime.datetime.now(datetime.timezone.utc)
certificate = (x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key())
    .serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5))
    .not_valid_after(now+datetime.timedelta(days=7)).add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
    .add_extension(x509.SubjectAlternativeName([x509.DNSName("localhost"), x509.IPAddress(ipaddress.ip_address("127.0.0.1"))]), critical=False)
    .sign(key, hashes.SHA256()))
(directory / "cert.pem").write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
(directory / "key.pem").write_bytes(key.private_bytes(serialization.Encoding.PEM, serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
(directory / "provider.private.json").write_text(json.dumps(dict(id="local-enterprise", label="本地企业账号", kind="oidc", issuer="https://localhost:24443", clientId=application["clientId"], clientSecret=application["clientSecret"], authorizeUrl="https://localhost:24443/login/oauth/authorize", tokenUrl="https://localhost:24443/api/login/oauth/access_token", jwksUrl="https://localhost:24443/.well-known/jwks", redirectUrl="https://localhost:24444/api/v1/auth/external/callback", tenantIds=["local-tenant"], reviewId="casdoor-v4.23.0-local-only")), encoding="utf-8")
print("Local fixture prepared. Generated secrets stay in the supplied private directory.")
