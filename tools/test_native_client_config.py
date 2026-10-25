"""Unit tests use synthetic certificates and keys, never a native provider."""

import base64
import contextlib
import io
import json
import os
from pathlib import Path
import struct
import tempfile
import unittest
from unittest.mock import patch

import lz4.block
from nacl.bindings import crypto_aead_chacha20poly1305_encrypt
from nacl.signing import SigningKey

import native_client_config as client


class NativeClientConfigTests(unittest.TestCase):
    def fixture(self):
        root = SigningKey(bytes(range(32)))
        first = SigningKey(bytes(range(1, 33)))
        second = SigningKey(bytes(range(2, 34)))
        first_public, second_public = bytes(first.verify_key), bytes(second.verify_key)
        cert_a = root.sign(struct.pack("<Q", 1) + first_public)
        cert_a = cert_a.message + cert_a.signature
        cert_b = first.sign(struct.pack("<Q", 5) + second_public)
        cert_b = cert_b.message + cert_b.signature
        plain = b"\x08\x01\x10\x01"  # project_env_id=1, line_group_id=1
        header = 1 | (1 << 2) | (3 << 6) | (len(plain) << 10) | (1 << 22)
        header_bytes, nonce = struct.pack("<Q", header), bytes(range(8))
        frame = lz4.block.compress(plain, store_size=False)
        sealed = crypto_aead_chacha20poly1305_encrypt(
            frame, header_bytes, nonce, client.derive_key(second_public, header)
        )
        prefix = header_bytes + nonce + sealed[-16:]
        body = prefix + second.sign(prefix).signature + sealed[:-16]
        chunks = [cert_a, cert_b, body]
        encode = lambda items: b".".join(base64.urlsafe_b64encode(item).rstrip(b"=") for item in items)
        return bytes(root.verify_key), chunks, encode

    def test_signed_bootstrap_and_tamper_rejection(self):
        root, chunks, encode = self.fixture()
        self.assertEqual(client.decode_bootstrap(encode(chunks), root), {1: [1], 2: [1]})
        for section, offset in ((0, 8), (1, 8), (2, 32), (2, -1)):
            damaged = list(chunks)
            part = bytearray(damaged[section])
            part[offset] ^= 1
            damaged[section] = bytes(part)
            with self.assertRaises(Exception):
                client.decode_bootstrap(encode(damaged), root)

    def test_bounded_parsers(self):
        self.assertEqual(client.proto(b"\x48\xff\xff\xff\xff\xff\xff\xff\xff\xff\x01"), {9: [2**64 - 1]})
        for value in (b"\x0a\x05x", b"\0", b"\x48" + b"\xff" * 10):
            with self.assertRaises(ValueError):
                client.proto(value)
        with self.assertRaises(ValueError):
            list(client.dex_strings(bytes(112)))

    def test_private_output_is_not_overwritten_or_printed(self):
        fixture = {"origins": ["https://example.invalid"], "body_key": b"b" * 32, "server_key": b"s" * 32}
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "private.json"
            argv = ["native_client_config.py", "synthetic.apk", "--output", str(output)]
            printed = io.StringIO()
            with patch.object(client, "read_client", return_value=fixture), patch("sys.argv", argv):
                with contextlib.redirect_stdout(printed):
                    client.main()
                self.assertEqual(output.stat().st_mode & 0o777, 0o600)
                original = output.read_bytes()
                deployment = json.loads(original)
                self.assertEqual(len(base64.b64decode(deployment["signing_seed"])), 32)
                self.assertEqual(deployment["metadata"], [0] * 14)
                for name in ("signing_seed", "body_key", "server_key", "device_id"):
                    self.assertNotIn(deployment[name], printed.getvalue())
                with self.assertRaises(FileExistsError):
                    client.main()
                self.assertEqual(output.read_bytes(), original)
                link = Path(directory) / "link.json"
                link.symlink_to(output)
                with patch("sys.argv", argv[:-1] + [str(link)]), self.assertRaises(FileExistsError):
                    client.main()

    @unittest.skipUnless(os.environ.get("WSL_CLIENT_APK"), "optional pinned APK check")
    def test_pinned_artifact(self):
        value = client.read_client(os.environ["WSL_CLIENT_APK"])
        self.assertEqual(len(value["origins"]), 3)
        self.assertEqual(value["app_hash"].split("=")[1], "5D499D6530B3C4DC2787F668CF85F7A676CB45B85E71F3FD8105006D1B732A45")
        self.assertEqual(len(value["server_key"]), 32)


if __name__ == "__main__":
    unittest.main()
