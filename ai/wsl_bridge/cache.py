from collections import OrderedDict
from dataclasses import dataclass
from time import monotonic

from .contracts import Scope, ScopeError


@dataclass(frozen=True, slots=True)
class Entry:
    value: bytes
    expires: float


class ScopeCache:
    """A bounded cache with no unscoped/global lookup or mutable shared values."""

    def __init__(self, capacity: int = 512):
        if not 1 <= capacity <= 4096:
            raise ValueError("invalid cache capacity")
        self._capacity = capacity
        self._entries: OrderedDict[tuple[str, str], Entry] = OrderedDict()

    @staticmethod
    def _key(scope: Scope, key: str) -> tuple[str, str]:
        if not isinstance(scope, Scope) or not key or len(key) > 200:
            raise ScopeError("explicit cache scope required")
        return scope.key, key

    def put(self, scope: Scope, key: str, value: bytes, ttl: float) -> None:
        cache_key = self._key(scope, key)
        if not 0 < ttl <= 7 * 24 * 60 * 60 or type(value) is not bytes or len(value) > 65536:
            raise ValueError("bounded cache value and retention required")
        self._entries[cache_key] = Entry(value, monotonic() + ttl)
        self._entries.move_to_end(cache_key)
        while len(self._entries) > self._capacity:
            self._entries.popitem(last=False)

    def get(self, scope: Scope, key: str) -> bytes | None:
        cache_key = self._key(scope, key)
        entry = self._entries.get(cache_key)
        if entry is None:
            return None
        if entry.expires <= monotonic():
            del self._entries[cache_key]
            return None
        self._entries.move_to_end(cache_key)
        return entry.value

    def delete_scope(self, scope: Scope) -> None:
        namespace, _ = self._key(scope, "scope")
        for key in list(self._entries):
            if key[0] == namespace:
                del self._entries[key]
