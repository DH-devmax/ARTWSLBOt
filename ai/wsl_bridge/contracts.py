from __future__ import annotations

import hashlib
import hmac
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from uuid import UUID


class ScopeError(ValueError):
    pass


def identifier(value: str) -> None:
    try:
        parsed = UUID(value)
    except (ValueError, TypeError, AttributeError) as exc:
        raise ScopeError("canonical UUID required") from exc
    if str(parsed) != value or parsed.int == 0:
        raise ScopeError("canonical nonempty UUID required")


@dataclass(frozen=True, slots=True)
class Scope:
    tenant_id: str
    group_id: str
    native_group_id: str
    account_id: str
    bot_id: str

    def __post_init__(self) -> None:
        for value in (self.tenant_id, self.group_id, self.account_id):
            identifier(value)
        for value in (self.native_group_id, self.bot_id):
            if not isinstance(value, str) or not value.strip() or len(value.encode()) > 200:
                raise ScopeError("native identity required")

    @property
    def key(self) -> str:
        # Length-delimited JSON avoids delimiter collisions between native IDs.
        value = json.dumps(
            [self.tenant_id, self.group_id, self.native_group_id, self.account_id, self.bot_id],
            separators=(",", ":"), ensure_ascii=True,
        ).encode()
        return hashlib.sha256(value).hexdigest()

    def person_key(self, native_member_id: str) -> str:
        if not native_member_id or len(native_member_id.encode()) > 200:
            raise ScopeError("native member identity required")
        return hashlib.sha256((self.key + ":" + native_member_id).encode()).hexdigest()

    @property
    def group_key(self) -> str:
        value = json.dumps([self.tenant_id, self.group_id, self.native_group_id], separators=(",", ":"))
        return hashlib.sha256(value.encode()).hexdigest()


@dataclass(frozen=True, slots=True)
class PersonaSnapshot:
    account_id: str
    version: int
    nickname: str
    personality: str
    reply_style: str
    aliases: tuple[str, ...] = ()

    def __post_init__(self) -> None:
        identifier(self.account_id)
        if type(self.version) is not int or self.version < 1:
            raise ScopeError("persona version required")
        for value, limit in ((self.nickname, 100), (self.personality, 12000), (self.reply_style, 4000)):
            if not isinstance(value, str) or len(value.encode()) > limit:
                raise ScopeError("invalid persona")
        if not self.nickname.strip() or type(self.aliases) is not tuple:
            raise ScopeError("immutable persona required")
        if len(self.aliases) > 20 or any(not isinstance(a, str) or len(a.encode()) > 100 for a in self.aliases):
            raise ScopeError("invalid persona aliases")


@dataclass(frozen=True, slots=True)
class Candidate:
    request_id: str
    scope: Scope
    persona: PersonaSnapshot
    binding_version: int
    account_epoch: int
    group_epoch: int
    expires_at: datetime
    source_event_ids: tuple[str, ...]

    def __post_init__(self) -> None:
        identifier(self.request_id)
        if not isinstance(self.scope, Scope) or not isinstance(self.persona, PersonaSnapshot):
            raise ScopeError("explicit scope and persona required")
        if self.scope.account_id != self.persona.account_id:
            raise ScopeError("persona belongs to another account")
        if any(type(v) is not int or v < 1 for v in (self.binding_version, self.account_epoch, self.group_epoch)):
            raise ScopeError("execution versions required")
        if self.expires_at.tzinfo is None:
            raise ScopeError("timezone-aware deadline required")
        if type(self.source_event_ids) is not tuple or not 1 <= len(self.source_event_ids) <= 64:
            raise ScopeError("bounded immutable event references required")
        if any(not isinstance(v, str) or not v or len(v.encode()) > 200 for v in self.source_event_ids):
            raise ScopeError("invalid event reference")

    def expired(self) -> bool:
        return self.expires_at <= datetime.now(timezone.utc)


@dataclass(frozen=True, slots=True)
class Reply:
    request_id: str
    text: str

    def __post_init__(self) -> None:
        identifier(self.request_id)
        if not isinstance(self.text, str) or not self.text.strip() or len(self.text.encode()) > 4000:
            raise ScopeError("bounded text-only AI reply required")


def authenticate(configured: str, supplied: str) -> bool:
    # An empty allowlist/token must never mean public access.
    return (
        isinstance(configured, str) and len(configured) >= 32
        and isinstance(supplied, str) and len(supplied) <= 256
        and hmac.compare_digest(configured.encode(), supplied.encode())
    )
