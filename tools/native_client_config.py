"""Generate private auth deployment configuration from one pinned official APK.

This tool performs no network requests and never runs or installs the client.
It deliberately rejects other releases instead of guessing offsets or keys.
"""

import argparse
import base64
import ctypes
import ctypes.util
import hashlib
import hmac
import io
import json
import os
from pathlib import Path
import secrets
import struct
from urllib.parse import urlsplit
import zipfile

from elftools.elf.elffile import ELFFile
import lz4.block
from nacl.bindings import (
    crypto_aead_chacha20poly1305_decrypt,
    crypto_sign_ed25519_pk_to_curve25519,
)
from nacl.signing import VerifyKey

APK_SHA256 = "8f9a1e4483b0dd210b1c2c6e9549776a103af777b08bc04a06252e2fbd91f96d"
APK_SIZE = 73335376
CERT_SHA256 = "3cf2352f6db285679825b12f065efc5c7aac0f6d7467da4ef9500558c442f196"
CAPTCHA_ID = "37ab6b750c3246af804132808408f398"
MAX_ENTRY = 32 * 1024 * 1024


def uleb(data, offset, bits=64):
    value = 0
    for shift in range(0, bits, 7):
        if offset >= len(data):
            raise ValueError("truncated varint")
        byte = data[offset]
        offset += 1
        if (bits == 32 and shift == 28 and byte > 15) or (bits == 64 and shift == 63 and byte > 1):
            raise ValueError("oversized varint")
        value |= (byte & 127) << shift
        if byte < 128:
            return value, offset
    raise ValueError("oversized varint")


def proto(data):
    if len(data) > 65536:
        raise ValueError("oversized bootstrap protobuf")
    fields = {}
    offset = 0
    while offset < len(data):
        tag, offset = uleb(data, offset)
        number, kind = tag >> 3, tag & 7
        if number == 0:
            raise ValueError("invalid protobuf tag")
        if kind == 0:
            value, offset = uleb(data, offset)
        elif kind == 2:
            size, offset = uleb(data, offset)
            if offset + size > len(data):
                raise ValueError("truncated protobuf")
            value, offset = data[offset:offset + size], offset + size
        else:
            raise ValueError("unexpected bootstrap field type")
        fields.setdefault(number, []).append(value)
    return fields


def dex_strings(data):
    if len(data) < 112 or not data.startswith(b"dex\n"):
        raise ValueError("invalid DEX")
    count, table = struct.unpack_from("<II", data, 56)
    if count > 2_000_000 or table + count * 4 > len(data):
        raise ValueError("invalid DEX string table")
    for index in range(count):
        offset, = struct.unpack_from("<I", data, table + index * 4)
        _, start = uleb(data, offset, 32)
        end = data.find(b"\0", start, min(start + 8193, len(data)))
        if end != -1:
            # Target constants are ASCII, so MUTF-8 surrogate handling is not needed.
            yield data[start:end]


def virtual_bytes(image, address, size):
    elf = ELFFile(io.BytesIO(image))
    for segment in elf.iter_segments():
        start, length = segment["p_vaddr"], segment["p_filesz"]
        if segment["p_type"] == "PT_LOAD" and start <= address and address + size <= start + length:
            offset = segment["p_offset"] + address - start
            return image[offset:offset + size]
    raise ValueError("unmapped pinned native constant")


def derive_key(root, subkey_id):
    name = ctypes.util.find_library("sodium")
    if not name:
        for candidate in ("/opt/homebrew/lib/libsodium.dylib", "/usr/local/lib/libsodium.dylib"):
            if Path(candidate).is_file():
                name = candidate
                break
    if not name:
        raise ValueError("libsodium is required")
    lib = ctypes.CDLL(name)
    lib.sodium_init.restype = ctypes.c_int
    if lib.sodium_init() < 0:
        raise ValueError("libsodium initialization failed")
    derive = lib.crypto_kdf_derive_from_key
    derive.argtypes = [ctypes.c_void_p, ctypes.c_size_t, ctypes.c_uint64,
                       ctypes.c_void_p, ctypes.c_void_p]
    derive.restype = ctypes.c_int
    output = ctypes.create_string_buffer(32)
    if derive(output, 32, subkey_id, struct.pack("<Q", 1), root) != 0:
        raise ValueError("bootstrap KDF failed")
    return output.raw


def decode_bootstrap(token, project_key):
    chunks = token.split(b".")
    if len(chunks) != 3:
        raise ValueError("invalid bootstrap envelope")
    cert_a, cert_b, body = (
        base64.urlsafe_b64decode(chunk + b"=" * (-len(chunk) % 4)) for chunk in chunks
    )
    if len(cert_a) != 104 or len(cert_b) != 104 or not 96 < len(body) <= 8192:
        raise ValueError("unsupported bootstrap certificate")
    VerifyKey(project_key).verify(cert_a[:-64], cert_a[-64:])
    key_a = cert_a[8:40]
    VerifyKey(key_a).verify(cert_b[:-64], cert_b[-64:])
    header, = struct.unpack_from("<Q", body)
    key = cert_b[8:40] if ((header >> 2) & 15) else key_a
    VerifyKey(key).verify(body[:32], body[32:96])
    size = (header >> 10) & 4095
    if not size:
        raise ValueError("empty bootstrap payload")
    compressed = crypto_aead_chacha20poly1305_decrypt(
        body[96:] + body[16:32], body[:8], body[8:16], derive_key(key, header)
    )
    plain = lz4.block.decompress(compressed, uncompressed_size=size)
    if len(plain) != size:
        raise ValueError("bootstrap length mismatch")
    fields = proto(plain)
    return fields


def read_client(path):
    if Path(path).stat().st_size != APK_SIZE:
        raise ValueError("unrecognized APK size")
    raw = Path(path).read_bytes()
    if hashlib.sha256(raw).hexdigest() != APK_SHA256:
        raise ValueError("unrecognized APK: offsets are valid only for the pinned 3.7.4 artifact")
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        def entry(name):
            info = archive.getinfo(name)
            if info.file_size > MAX_ENTRY:
                raise ValueError("oversized APK entry")
            return archive.read(info)

        security = entry("lib/arm64-v8a/libsecurity.so")
        # security_getProjectPk and production security_getServerPk(1).
        project_key = virtual_bytes(security, 0x42F00, 16) + virtual_bytes(security, 0x42FF0, 16)
        signing_server = virtual_bytes(security, 0x42F10, 16) + virtual_bytes(security, 0x42F90, 16)
        server_key = crypto_sign_ed25519_pk_to_curve25519(signing_server)
        # security_yntb streams sourceDir (+ any split APKs) through HmacSHA256.
        identity_key = virtual_bytes(security, 0x43030, 16) + virtual_bytes(security, 0x42FD0, 16)
        identity = hmac.new(identity_key, raw, hashlib.sha256).hexdigest().upper()
        candidates = []
        captcha_found = False
        for name in archive.namelist():
            if name.startswith("classes") and name.endswith(".dex") and "/" not in name:
                for value in dex_strings(entry(name)):
                    captcha_found |= value == CAPTCHA_ID.encode()
                    if 400 < len(value) < 8192 and value.count(b".") == 2:
                        try:
                            fields = decode_bootstrap(value, project_key)
                        except Exception:
                            # Most three-part DEX strings are not bootstrap records.
                            continue
                        if fields.get(1) == [1] and fields.get(2) == [1] and fields not in candidates:
                            candidates.append(fields)
        if not captcha_found or len(candidates) != 1:
            raise ValueError("production bootstrap or captcha identity is ambiguous")
        fields = candidates[0]
        lines = proto(fields[6][0]).get(1, [])
        origins = []
        for line in lines:
            record = proto(line)
            domain = record.get(5, [b""])[0].decode("ascii")
            origin = "https://" + domain
            parsed = urlsplit(origin)
            if (not parsed.hostname or parsed.username or parsed.password or parsed.path or
                    parsed.query or parsed.fragment or "\\" in origin or
                    any(ord(c) < 33 or ord(c) > 126 for c in origin)):
                raise ValueError("invalid signed business origin")
            # The pinned SDK's kv2.getFullURL always selects HTTPS.
            if origin not in origins:
                origins.append(origin)
        if not origins:
            raise ValueError("production bootstrap has no business origins")
        return {
            "origins": origins, "body_key": project_key, "server_key": server_key,
            "app_hash": ":".join(CERT_SHA256[i:i + 2].upper() for i in range(0, 64, 2)) + "=" + identity,
        }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("apk", type=Path)
    parser.add_argument("--output", type=Path, help="new private JSON file; existing files are never overwritten")
    parser.add_argument("--origin", help="select one of the signed business origins")
    args = parser.parse_args()
    client = read_client(args.apk)
    origin = args.origin or client["origins"][0]
    if origin not in client["origins"]:
        raise ValueError("origin is not present in the signed production bootstrap")
    summary = {"artifact_sha256": APK_SHA256, "protocol_version": "3.7.4",
               "signed_bootstrap_verified": True, "origin": origin,
               "candidate_origins": client["origins"], "network_requests": 0}
    if args.output:
        encoded = lambda value: base64.b64encode(value).decode("ascii")
        deployment = {
            "origin": origin, "headers": {}, "metadata": [0] * 14,
            "signing_seed": encoded(secrets.token_bytes(32)),
            "body_key": encoded(client["body_key"]), "message_key": encoded(client["body_key"]),
            "server_key": encoded(client["server_key"]), "captcha_id": CAPTCHA_ID,
            "app_key": "", "device_id": secrets.token_hex(16), "protocol_version": "3.7.4",
        }
        # Reject symlinks, races, and permissive existing files via O_EXCL.
        fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w") as output:
            json.dump(deployment, output, indent=2)
            output.write("\n")
        summary["configuration_file"] = str(args.output.resolve())
        summary["im_transport_configured"] = False
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Native client inspection failed; no credentials were printed. Check the pinned artifact and tool dependencies.")
