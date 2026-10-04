"""
Compatibility test: real Azure SDK against Azure Local.

This test is the contract. If it does not pass, Blob is not supported.
"""
import hashlib
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


# ---- container ops --------------------------------------------------------

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


# ---- blob ops -------------------------------------------------------------

def test_upload_download_roundtrip(client):
    container = "pytest-rt-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        blob = client.get_blob_client(container, "hello.txt")
        data = b"hello, azure-local\n" * 1000  # 19 KB
        blob.upload_blob(data, overwrite=True)

        got = blob.download_blob().readall()
        assert got == data
        assert hashlib.sha256(got).hexdigest() == hashlib.sha256(data).hexdigest()
    finally:
        client.delete_container(container)


def test_upload_large_block_blob(client):
    """Force the SDK to use block-based upload."""
    container = "pytest-blk-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        blob = client.get_blob_client(container, "large.bin")
        data = os.urandom(5 * 1024 * 1024)
        blob.upload_blob(
            data,
            overwrite=True,
            max_single_put_size=1 * 1024 * 1024,
            max_block_size=1 * 1024 * 1024,
        )
        got = blob.download_blob().readall()
        assert len(got) == len(data)
        assert hashlib.sha256(got).hexdigest() == hashlib.sha256(data).hexdigest()
    finally:
        client.delete_container(container)


def test_blob_properties(client):
    container = "pytest-prop-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        blob = client.get_blob_client(container, "notes.txt")
        blob.upload_blob(b"hello", overwrite=True, content_type="text/plain")
        props = blob.get_blob_properties()
        assert props.size == 5
        assert props.content_settings.content_type == "text/plain"
    finally:
        client.delete_container(container)


def test_blob_range_download(client):
    container = "pytest-rng-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        blob = client.get_blob_client(container, "alphabet.txt")
        blob.upload_blob(b"abcdefghij", overwrite=True)
        partial = blob.download_blob(offset=2, length=4).readall()
        assert partial == b"cdef"
    finally:
        client.delete_container(container)


def test_list_blobs_with_prefix(client):
    container = "pytest-lb-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        for name in ["docs/a.txt", "docs/b.txt", "images/c.png"]:
            client.get_blob_client(container, name).upload_blob(b"x", overwrite=True)
        names = [
            b.name
            for b in client.get_container_client(container).list_blobs(name_starts_with="docs/")
        ]
        assert "docs/a.txt" in names
        assert "docs/b.txt" in names
        assert "images/c.png" not in names
    finally:
        client.delete_container(container)


def test_delete_blob(client):
    container = "pytest-del-" + uuid.uuid4().hex[:8]
    client.create_container(container)
    try:
        blob = client.get_blob_client(container, "gone.txt")
        blob.upload_blob(b"x", overwrite=True)
        blob.delete_blob()
        with pytest.raises(ResourceNotFoundError):
            blob.download_blob().readall()
    finally:
        client.delete_container(container)
