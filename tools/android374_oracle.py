"""Offline oracle: emulate pinned ARM64 exchange/token routines, with synthetic keys.

No APK installation, JNI initialization, networking, login, or SMS is performed.
Only allocator/memory operations and libsodium calls are supplied by the host.
"""

import argparse
import hashlib
import io
import json
import struct
import zipfile

from elftools.elf.elffile import ELFFile
import mmh3
from nacl.bindings import crypto_aead_chacha20poly1305_encrypt, crypto_box_seal
from unicorn import Uc, UC_ARCH_ARM64, UC_MODE_ARM, UC_HOOK_CODE
from unicorn.arm64_const import (
    UC_ARM64_REG_X0, UC_ARM64_REG_X8, UC_ARM64_REG_LR, UC_ARM64_REG_PC,
    UC_ARM64_REG_SP, UC_ARM64_REG_TPIDR_EL0,
)

from native_client_config import APK_SHA256


class Oracle:
    BASE, HEAP, STACK, HOOKS = 0x100000, 0x1000000, 0x2000000, 0x3000000

    def __init__(self, image):
        self.uc = Uc(UC_ARCH_ARM64, UC_MODE_ARM)
        self.uc.mem_map(self.BASE, 0x100000)
        self.uc.mem_map(self.HEAP, 0x800000)
        self.uc.mem_map(self.STACK, 0x100000)
        self.uc.mem_map(self.HOOKS, 0x10000)
        self.next = self.HEAP
        self.hooks = {}
        self.bindings = []
        self.tokens = []
        elf = ELFFile(io.BytesIO(image))
        for segment in elf.iter_segments():
            if segment["p_type"] == "PT_LOAD":
                self.uc.mem_write(self.BASE + segment["p_vaddr"], segment.data())
        for section in elf.iter_sections():
            if section["sh_type"] != "SHT_RELA":
                continue
            symbols = elf.get_section(section["sh_link"])
            for relocation in section.iter_relocations():
                target = self.BASE + relocation["r_offset"]
                kind, addend = relocation["r_info_type"], relocation["r_addend"]
                if kind == 1027:  # R_AARCH64_RELATIVE
                    value = self.BASE + addend
                elif kind in (257, 1025, 1026):
                    symbol = symbols.get_symbol(relocation["r_info_sym"])
                    if symbol["st_shndx"] != "SHN_UNDEF":
                        value = self.BASE + symbol["st_value"] + addend
                    else:
                        value = self.HOOKS + 16 * (len(self.hooks) + 1)
                        self.hooks[value] = symbol.name
                else:
                    raise ValueError("unsupported native relocation")
                self.uc.mem_write(target, struct.pack("<Q", value))
        self.uc.hook_add(UC_HOOK_CODE, self.external, begin=self.HOOKS, end=self.HOOKS + 0xFFFF)
        self.uc.reg_write(UC_ARM64_REG_TPIDR_EL0, self.STACK)

    def allocate(self, size):
        if not 0 <= size <= 2 * 1024 * 1024:
            raise ValueError("unbounded native allocation")
        pointer = self.next
        self.next += (max(size, 1) + 15) & ~15
        if self.next >= self.HEAP + 0x800000:
            raise ValueError("oracle heap exhausted")
        return pointer

    def put(self, data):
        pointer = self.allocate(len(data))
        if data:
            self.uc.mem_write(pointer, data)
        return pointer

    def read(self, pointer, size):
        return bytes(self.uc.mem_read(pointer, size))

    def external(self, uc, address, size, data):
        if address == self.HOOKS:
            uc.emu_stop()
            return
        name = self.hooks.get(address)
        args = [uc.reg_read(UC_ARM64_REG_X0 + i) for i in range(8)]
        result = 0
        if name in ("_Znwm", "malloc"):
            result = self.allocate(args[0])
        elif name in ("_ZdlPv", "_ZdaPv", "free"):
            pass
        elif name in ("memcpy", "memmove"):
            uc.mem_write(args[0], self.read(args[1], args[2]))
            result = args[0]
        elif name == "memset":
            uc.mem_write(args[0], bytes([args[1] & 255]) * args[2])
            result = args[0]
        elif name == "crypto_aead_chacha20poly1305_encrypt_detached":
            nonce, key = struct.unpack("<QQ", self.read(uc.reg_read(UC_ARM64_REG_SP), 16))
            plain, aad = self.read(args[3], args[4]), self.read(args[5], args[6])
            sealed = crypto_aead_chacha20poly1305_encrypt(
                plain, aad, self.read(nonce, 8), self.read(key, 32)
            )
            uc.mem_write(args[0], sealed[:-16])
            uc.mem_write(args[1], sealed[-16:])
            uc.mem_write(args[2], struct.pack("<Q", 16))
            self.bindings.append({"aad_hex": aad.hex(), "nonce_hex": self.read(nonce, 8).hex()})
        elif name == "crypto_box_seal":
            plain = self.read(args[1], args[2])
            self.tokens.append(plain.hex())
            uc.mem_write(args[0], crypto_box_seal(plain, self.read(args[3], 32)))
        else:
            raise ValueError("unexpected native external function: " + str(name))
        uc.reg_write(UC_ARM64_REG_X0, result)
        uc.reg_write(UC_ARM64_REG_PC, uc.reg_read(UC_ARM64_REG_LR))

    def call(self, address, args, result_size=24, stack_args=()):
        output = self.put(bytes(result_size))
        stack = self.STACK + 0xF0000
        if stack_args:
            self.uc.mem_write(stack, struct.pack("<" + "Q" * len(stack_args), *stack_args))
        for i, arg in enumerate(args):
            self.uc.reg_write(UC_ARM64_REG_X0 + i, arg)
        self.uc.reg_write(UC_ARM64_REG_X8, output)
        self.uc.reg_write(UC_ARM64_REG_SP, stack)
        self.uc.reg_write(UC_ARM64_REG_LR, self.HOOKS)
        self.uc.emu_start(self.BASE + address, self.HOOKS, timeout=5_000_000, count=2_000_000)
        if self.uc.reg_read(UC_ARM64_REG_PC) != self.HOOKS:
            raise ValueError("native routine did not finish within oracle limits")
        return self.read(output, result_size)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("apk")
    args = parser.parse_args()
    raw = open(args.apk, "rb").read()
    if hashlib.sha256(raw).hexdigest() != APK_SHA256:
        raise ValueError("refusing an unpinned client")
    image = zipfile.ZipFile(io.BytesIO(raw)).read("lib/arm64-v8a/libcore.so")
    oracle = Oracle(image)
    plain, key = b'{"synthetic":true}', bytes(range(32))
    frame = bytes.fromhex("28b52ffda0") + struct.pack("<I", len(plain))
    frame += ((len(plain) << 3) | 1).to_bytes(3, "little") + plain
    digest, _ = mmh3.mmh3_x64_128_utupledigest(plain)
    success = oracle.put(b"\0")
    result = oracle.call(0x2C3A8, [oracle.put(frame), len(frame), 42, digest, 99,
                                    1703240058617, oracle.put(key), success])
    start, end, _ = struct.unpack("<QQQ", result)
    if oracle.read(success, 1) != b"\1" or not 32 < end - start < 4096:
        raise ValueError("native exchange encoder failed")
    # Also execute the native token serializer and inspect only synthetic plaintext.
    public = bytes(range(32))
    server = bytes.fromhex("09" + "00" * 31)
    token = oracle.call(0x2DAE4, [99, 3, oracle.put(public), 32, 1700000000,
                                  1700000000123456789, 1, 0],
                        result_size=32, stack_args=(oracle.put(server),))
    if token[0] != 0 or len(oracle.tokens) != 1:
        raise ValueError("native context serializer failed")
    print(json.dumps({
        "apk_sha256": APK_SHA256, "native_exchange_offset": "0x2c3a8",
        "native_context_offset": "0x2dae4", "plain_hex": plain.hex(),
        "key_hex": key.hex(), "request_id": 42, "device": 99,
        "version": 1703240058617, "body_digest": digest,
        "body_hex": oracle.read(start, end - start).hex(),
        "binding": oracle.bindings[0], "context_plain_hex": oracle.tokens[0],
        "network_requests": 0,
    }, indent=2))


if __name__ == "__main__":
    main()
