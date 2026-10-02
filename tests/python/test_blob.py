"""
Compatibility test: real Azure SDK against Azure Local.

This test is the contract. If it does not pass, Blob is not supported.
"""
import os
import uuid

import pytest
from azure.core.exceptions import ResourceExistsError, ResourceNotFoundError
from azure.storage.blob import BlobServiceClient

CONN_STR = os.environ["AZURE_STORAGE_CONNECTION_STRING"]


@pytest.fixture(scope="module")
def client() -> BlobServiceClient:
    return BlobServiceClient.from_connection_string(CONN_STR)


@pytest.fixture
def container_name() -> str:
    return "pytest-" + uuid.uuid4().hex[:12]


def test_health_endpoint_reachable():
    import urllib.request
    with urllib.request.urlopen("http://127.0.0.1:4577/health", timeout=2) as r:
        assert r.status == 200


def test_create_and_list_container(client, container_name):
    try:
        client.delete_container(container_name)
    except ResourceNotFoundError:
        pass

    created = client.create_container(container_name)
    assert created is not None

    names = [c.name for c in client.list_containers()]
    assert container_name in names

    # cleanup
    client.delete_container(container_name)


def test_create_existing_container_conflicts(client, container_name):
    client.create_container(container_name)
    with pytest.raises(ResourceExistsError):
        client.create_container(container_name)
    client.delete_container(container_name)


def test_get_container_properties(client, container_name):
    client.create_container(container_name, metadata={"owner": "pytest"})
    props = client.get_container_properties(container_name)
    assert props.metadata.get("owner") == "pytest"
    client.delete_container(container_name)


def test_list_with_prefix(client):
    a = "pytest-a-" + uuid.uuid4().hex[:6]
    b = "pytest-b-" + uuid.uuid4().hex[:6]
    client.create_container(a)
    client.create_container(b)
    try:
        names = [c.name for c in client.list_containers(name_starts_with="pytest-a-")]
        assert a in names
        assert b not in names
    finally:
        client.delete_container(a)
        client.delete_container(b)
