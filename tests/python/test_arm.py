"""
ARM control plane compatibility tests.

These use plain HTTP (urllib) because the Azure SDK requires Identity
(Milestone 4). The contract exercised here is the same one the SDK and the
`az` CLI will use once Identity exists.
"""
import json
import os
import urllib.request
import urllib.error

BASE = os.environ.get("AZLOCAL_ENDPOINT", "http://127.0.0.1:4577")
SUB = "local-sub"
API = "api-version=2022-09-01"


def _req(method, path, body=None):
    url = f"{BASE}{path}"
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            raw = r.read()
            return r.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        raw = e.read()
        return e.code, json.loads(raw) if raw else None


def test_metadata_endpoints():
    code, body = _req("GET", "/metadata/endpoints?api-version=2022-09-01")
    assert code == 200
    assert "resourceManager" in body
    assert body["resourceManager"].startswith("http://")


def test_list_subscriptions():
    code, body = _req("GET", f"/subscriptions?{API}")
    assert code == 200
    ids = [s["subscriptionId"] for s in body["value"]]
    assert SUB in ids


def test_resource_group_crud():
    rg = "demo-rg"
    rg_url = f"/subscriptions/{SUB}/resourceGroups/{rg}?{API}"

    # Cleanup leftovers.
    _req("DELETE", rg_url)

    code, body = _req("PUT", rg_url, {"location": "westeurope"})
    assert code == 201
    assert body["name"] == rg
    assert body["location"] == "westeurope"
    assert body["properties"]["provisioningState"] == "Succeeded"

    code, body = _req("GET", rg_url)
    assert code == 200
    assert body["name"] == rg

    code, body = _req("GET", f"/subscriptions/{SUB}/resourceGroups?{API}")
    assert code == 200
    assert any(x["name"] == rg for x in body["value"])

    code, _ = _req("DELETE", rg_url)
    assert code == 200

    code, _ = _req("GET", rg_url)
    assert code == 404


def test_storage_account_lifecycle():
    rg = "strg-rg"
    rg_url = f"/subscriptions/{SUB}/resourceGroups/{rg}?{API}"
    _req("DELETE", rg_url)
    _req("PUT", rg_url, {"location": "westeurope"})

    sa_url = (
        f"/subscriptions/{SUB}/resourceGroups/{rg}"
        f"/providers/Microsoft.Storage/storageAccounts/myacct?api-version=2023-01-01"
    )
    _req("DELETE", sa_url)

    code, body = _req("PUT", sa_url, {
        "location": "westeurope",
        "sku": {"name": "Standard_LRS"},
        "kind": "StorageV2",
    })
    assert code in (200, 201)
    assert body["name"] == "myacct"
    assert body["type"] == "Microsoft.Storage/storageAccounts"
    assert body["properties"]["provisioningState"] == "Succeeded"
    assert body["properties"]["primaryEndpoints"]["blob"].startswith("http://")

    code, body = _req("GET", sa_url)
    assert code == 200
    assert body["name"] == "myacct"

    code, body = _req(
        "GET",
        f"/subscriptions/{SUB}/resourceGroups/{rg}/resources?{API}",
    )
    assert code == 200
    assert any(r["name"] == "myacct" for r in body["value"])

    code, _ = _req("DELETE", sa_url)
    assert code == 200

    # Cleanup.
    _req("DELETE", rg_url)


def test_404_on_missing_rg():
    code, body = _req(
        "GET",
        f"/subscriptions/{SUB}/resourceGroups/does-not-exist?{API}",
    )
    assert code == 404
    assert body["error"]["code"] == "ResourceGroupNotFound"
