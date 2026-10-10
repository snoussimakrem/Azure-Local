"""
RBAC and Policy compatibility tests.

Plain-HTTP, because these endpoints are pure ARM-shaped JSON. The Azure SDK
would use them the same way once Terraform and the CLI can authenticate.
"""
import json
import os
import urllib.error
import urllib.request

BASE = os.environ.get("AZLOCAL_ENDPOINT", "http://127.0.0.1:4577")
SUB = "local-sub"
API = "api-version=2022-04-01"


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
        try:
            parsed = json.loads(raw) if raw else None
        except Exception:
            parsed = raw.decode(errors="replace")
        return e.code, parsed


def _cleanup_role_assignment(name):
    for scope in (
        f"/subscriptions/{SUB}",
        f"/subscriptions/{SUB}/resourceGroups/demo",
    ):
        _req("DELETE", f"{scope}/providers/Microsoft.Authorization/roleAssignments/{name}?{API}")


def _cleanup_policy_assignment(name):
    for scope in (
        f"/subscriptions/{SUB}",
        f"/subscriptions/{SUB}/resourceGroups/demo",
    ):
        _req("DELETE", f"{scope}/providers/Microsoft.Authorization/policyAssignments/{name}?{API}")


# ---- RBAC -----------------------------------------------------------------

def test_list_builtin_role_definitions():
    code, body = _req("GET", f"/subscriptions/{SUB}/providers/Microsoft.Authorization/roleDefinitions?{API}")
    assert code == 200
    names = [r["properties"]["roleName"] for r in body["value"]]
    assert "Owner" in names
    assert "Contributor" in names
    assert "Reader" in names
    assert "Storage Blob Data Contributor" in names


def test_create_and_delete_role_assignment():
    name = "test-assign-1"
    _cleanup_role_assignment(name)

    scope = f"/subscriptions/{SUB}"
    url = f"{scope}/providers/Microsoft.Authorization/roleAssignments/{name}?{API}"

    code, body = _req("PUT", url, {
        "properties": {
            "roleDefinitionId": f"{scope}/providers/Microsoft.Authorization/roleDefinitions/acdd72a7-3385-48ef-bd42-f606fba81ae7",
            "principalId": "some-principal",
            "principalType": "ServicePrincipal",
            "scope": scope,
        }
    })
    assert code in (200, 201), body
    assert body["name"] == name
    assert body["properties"]["principalId"] == "some-principal"

    code, body = _req("GET", url)
    assert code == 200
    assert body["name"] == name

    code, body = _req("GET", f"{scope}/providers/Microsoft.Authorization/roleAssignments?{API}")
    assert code == 200
    assert any(x["name"] == name for x in body["value"])

    code, _ = _req("DELETE", url)
    assert code == 200


# ---- Policy ---------------------------------------------------------------

def test_list_builtin_policy_definitions():
    code, body = _req("GET", f"/subscriptions/{SUB}/providers/Microsoft.Authorization/policyDefinitions?{API}")
    assert code == 200
    names = [d["properties"]["displayName"] for d in body["value"]]
    assert "Allowed locations" in names
    assert "Require a tag on resources" in names


def test_policy_denies_disallowed_location():
    name = "test-allow-west"
    _cleanup_policy_assignment(name)
    scope = f"/subscriptions/{SUB}"
    url = f"{scope}/providers/Microsoft.Authorization/policyAssignments/{name}?{API}"

    code, body = _req("PUT", url, {
        "properties": {
            "policyDefinitionId": "/providers/Microsoft.Authorization/policyDefinitions/e56962a6-4747-49cd-b67b-bf8b01975c4c",
            "scope": scope,
            "parameters": {
                "listOfAllowedLocations": {"value": ["westeurope"]}
            },
        }
    })
    assert code in (200, 201), body

    # westeurope allowed -> create RG
    code, _ = _req("PUT",
        f"{scope}/resourceGroups/policy-demo?api-version=2022-09-01",
        {"location": "westeurope"})
    assert code in (200, 201)

    # eastus denied
    code, body = _req("PUT",
        f"{scope}/resourceGroups/policy-demo-east?api-version=2022-09-01",
        {"location": "eastus"})
    assert code == 403, body
    assert body["error"]["code"] == "RequestDisallowedByPolicy"

    # cleanup
    _req("DELETE", f"{scope}/resourceGroups/policy-demo?api-version=2022-09-01")
    _req("DELETE", f"{scope}/resourceGroups/policy-demo-east?api-version=2022-09-01")
    _req("DELETE", url)


def test_policy_require_tag():
    name = "test-require-env"
    _cleanup_policy_assignment(name)
    scope = f"/subscriptions/{SUB}"
    url = f"{scope}/providers/Microsoft.Authorization/policyAssignments/{name}?{API}"

    code, _ = _req("PUT", url, {
        "properties": {
            "policyDefinitionId": "/providers/Microsoft.Authorization/policyDefinitions/871b6d14-10aa-478d-b590-94f262ecfa99",
            "scope": scope,
            "parameters": {"tagName": {"value": "env"}},
        }
    })
    assert code in (200, 201)

    # Without tag -> denied
    code, body = _req("PUT",
        f"{scope}/resourceGroups/tag-demo?api-version=2022-09-01",
        {"location": "westeurope"})
    assert code == 403, body
    assert body["error"]["code"] == "RequestDisallowedByPolicy"

    # With tag -> allowed
    code, _ = _req("PUT",
        f"{scope}/resourceGroups/tag-demo?api-version=2022-09-01",
        {"location": "westeurope", "tags": {"env": "prod"}})
    assert code in (200, 201)

    _req("DELETE", f"{scope}/resourceGroups/tag-demo?api-version=2022-09-01")
    _req("DELETE", url)
