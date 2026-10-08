"""
Local Entra ID compatibility tests.

Exercises the OIDC discovery document, JWKS endpoint, and the OAuth 2.0
token endpoint with the client_credentials grant. Validates the resulting
JWT is signed, well-formed, and carries the expected Azure-shaped claims.
"""
import base64
import json
import os
import urllib.parse
import urllib.request
import urllib.error

BASE = os.environ.get("AZLOCAL_ENDPOINT", "http://127.0.0.1:4577")
TENANT = "local"
CLIENT_ID = "local-client"
CLIENT_SECRET = "local-secret"


def _get(path):
    req = urllib.request.Request(BASE + path, headers={"Accept": "application/json"})
    with urllib.request.urlopen(req, timeout=5) as r:
        return r.status, json.loads(r.read())


def _post_form(path, form):
    data = urllib.parse.urlencode(form).encode()
    req = urllib.request.Request(
        BASE + path, data=data, method="POST",
        headers={"Content-Type": "application/x-www-form-urlencoded"},
    )
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return r.status, json.loads(r.read())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read())


def _decode_jwt(token):
    parts = token.split(".")
    assert len(parts) == 3, "JWT must have 3 segments"
    def pad(s): return s + "=" * (-len(s) % 4)
    header = json.loads(base64.urlsafe_b64decode(pad(parts[0])))
    payload = json.loads(base64.urlsafe_b64decode(pad(parts[1])))
    return header, payload


def test_oidc_discovery():
    code, doc = _get(f"/{TENANT}/v2.0/.well-known/openid-configuration")
    assert code == 200
    assert doc["issuer"].endswith(f"/{TENANT}/v2.0")
    assert doc["jwks_uri"].endswith(f"/{TENANT}/discovery/v2.0/keys")
    assert doc["token_endpoint"].endswith(f"/{TENANT}/oauth2/v2.0/token")
    assert "RS256" in doc["id_token_signing_alg_values_supported"]


def test_jwks():
    code, jwks = _get(f"/{TENANT}/discovery/v2.0/keys")
    assert code == 200
    keys = jwks["keys"]
    assert len(keys) == 1
    k = keys[0]
    assert k["kty"] == "RSA"
    assert k["alg"] == "RS256"
    assert k["use"] == "sig"
    assert k["e"] == "AQAB"  # 65537
    assert len(k["n"]) > 100


def test_client_credentials_token():
    code, resp = _post_form(f"/{TENANT}/oauth2/v2.0/token", {
        "grant_type": "client_credentials",
        "client_id": CLIENT_ID,
        "client_secret": CLIENT_SECRET,
        "scope": f"{BASE}/.default",
    })
    assert code == 200, resp
    assert resp["token_type"] == "Bearer"
    assert resp["expires_in"] == 3600
    assert "access_token" in resp

    header, claims = _decode_jwt(resp["access_token"])
    assert header["alg"] == "RS256"
    assert header["typ"] == "JWT"
    assert "kid" in header
    assert claims["tid"] == "local"
    assert claims["appid"] == CLIENT_ID
    assert claims["ver"] == "2.0"
    assert "Contributor" in claims["roles"]


def test_bad_client_secret():
    code, resp = _post_form(f"/{TENANT}/oauth2/v2.0/token", {
        "grant_type": "client_credentials",
        "client_id": CLIENT_ID,
        "client_secret": "wrong",
        "scope": f"{BASE}/.default",
    })
    assert code == 401
    assert resp["error"] == "invalid_client"


def test_unsupported_grant():
    code, resp = _post_form(f"/{TENANT}/oauth2/v2.0/token", {
        "grant_type": "authorization_code",
        "client_id": CLIENT_ID,
    })
    assert code == 400
    assert resp["error"] == "unsupported_grant_type"


def test_password_grant():
    code, resp = _post_form(f"/{TENANT}/oauth2/v2.0/token", {
        "grant_type": "password",
        "client_id": CLIENT_ID,
        "username": "admin@local",
        "password": "local",
        "scope": f"{BASE}/.default",
    })
    assert code == 200, resp
    _, claims = _decode_jwt(resp["access_token"])
    assert claims["name"] == "admin@local"
