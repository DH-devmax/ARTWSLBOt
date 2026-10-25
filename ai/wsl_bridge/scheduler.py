from __future__ import annotations

import asyncio
from collections import OrderedDict
from collections.abc import Awaitable, Callable

from .contracts import Candidate, Reply, Scope, ScopeError

Generate = Callable[[Candidate], Awaitable[Reply]]
Authorize = Callable[[Candidate], Awaitable[bool]]
Deliver = Callable[[Candidate, Reply], Awaitable[None]]


class Dispatcher:
    """Generation orchestration only; never performs a native platform action."""

    def __init__(self, generate: Generate, authorize: Authorize, deliver: Deliver, limit: int = 4):
        if not 1 <= limit <= 4:
            raise ValueError("at most four in-flight generations")
        self._generate, self._authorize, self._deliver = generate, authorize, deliver
        self._limit = limit
        self._pending: OrderedDict[str, Candidate] = OrderedDict()
        self._running: dict[str, asyncio.Task] = {}
        self._event = asyncio.Event()
        self._closed = False
        self.outcomes: dict[str, int] = {}

    def _count(self, code: str) -> None:
        self.outcomes[code] = self.outcomes.get(code, 0) + 1

    def submit(self, candidate: Candidate) -> str:
        if not isinstance(candidate, Candidate):
            raise ScopeError("candidate required")
        if self._closed:
            return "closed"
        if candidate.expired():
            self._count("expired")
            return "expired"
        key = candidate.scope.group_key
        if key not in self._pending and key not in self._running and len(set(self._pending) | set(self._running)) >= 30:
            self._count("capacity_rejected")
            return "capacity_rejected"
        replaced = key in self._pending
        # The latest candidate references durable inbox events; raw events are never deleted.
        self._pending[key] = candidate
        self._event.set()
        self._count("coalesced" if replaced else "enqueued")
        return "coalesced" if replaced else "enqueued"

    def cancel_scope(self, scope: Scope) -> None:
        if not isinstance(scope, Scope):
            raise ScopeError("scope required")
        self._pending.pop(scope.group_key, None)
        task = self._running.get(scope.group_key)
        if task:
            task.cancel()

    async def run(self) -> None:
        try:
            while not self._closed:
                await self._event.wait()
                self._event.clear()
                for key in list(self._pending):
                    if len(self._running) >= self._limit:
                        break
                    if key in self._running:
                        continue
                    candidate = self._pending.pop(key)
                    if candidate.expired():
                        self._count("expired")
                        continue
                    self._running[key] = asyncio.create_task(self._one(key, candidate))
        finally:
            await self.close()

    async def _one(self, key: str, candidate: Candidate) -> None:
        try:
            from datetime import datetime, timezone
            remaining = (candidate.expires_at - datetime.now(timezone.utc)).total_seconds()
            async with asyncio.timeout(max(0, remaining)):
                if not await self._authorize(candidate):
                    self._count("revoked")
                    return
                reply = await self._generate(candidate)
                if not isinstance(reply, Reply) or reply.request_id != candidate.request_id:
                    raise ScopeError("AI reply/request mismatch")
                if candidate.expired() or not await self._authorize(candidate):
                    self._count("revoked")
                    return
                # Go must perform its own final permission/version validation at native submission.
                await self._deliver(candidate, reply)
                self._count("delivered")
        except asyncio.CancelledError:
            self._count("cancelled")
            raise
        except TimeoutError:
            self._count("expired")
        except Exception:
            # Do not persist prompts, replies, or provider exception bodies in metrics/logs.
            self._count("failed")
        finally:
            self._running.pop(key, None)
            self._event.set()

    async def close(self) -> None:
        self._closed = True
        self._pending.clear()
        tasks = tuple(self._running.values())
        for task in tasks:
            task.cancel()
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)
        self._event.set()
