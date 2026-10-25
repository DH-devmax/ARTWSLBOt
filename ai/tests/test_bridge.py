import asyncio
import dataclasses
import unittest
from datetime import datetime, timedelta, timezone
from uuid import uuid4

from wsl_bridge.cache import ScopeCache
from wsl_bridge.compatibility import CURRENT
from wsl_bridge.contracts import Candidate, PersonaSnapshot, Reply, Scope, ScopeError, authenticate
from wsl_bridge.scheduler import Dispatcher


def scope(group=None, account=None):
    return Scope(str(uuid4()), group or str(uuid4()), "native-group", account or str(uuid4()), "native-bot")


def candidate(s, nickname="robot", **overrides):
    values = dict(
        request_id=str(uuid4()), scope=s,
        persona=PersonaSnapshot(s.account_id, 1, nickname, "test persona", "test style"),
        binding_version=1, account_epoch=1, group_epoch=1,
        expires_at=datetime.now(timezone.utc) + timedelta(seconds=10),
        source_event_ids=("event-1",),
    )
    values.update(overrides)
    return Candidate(**values)

async def wait_until(predicate):
    async with asyncio.timeout(3):
        while not predicate():
            await asyncio.sleep(0.001)


class ContractTests(unittest.TestCase):
    def test_same_member_is_not_shared_between_groups_or_tenants(self):
        a = scope()
        b = dataclasses.replace(a, group_id=str(uuid4()))
        c = dataclasses.replace(a, tenant_id=str(uuid4()))
        d = dataclasses.replace(a, account_id=str(uuid4()), bot_id="other-bot")
        keys = {s.person_key("same-human") for s in (a, b, c, d)}
        self.assertEqual(len(keys), 4)
        with self.assertRaises(ScopeError):
            dataclasses.replace(a, tenant_id="")

    def test_persona_is_immutable_and_account_bound(self):
        a = candidate(scope(), "A")
        with self.assertRaises(dataclasses.FrozenInstanceError):
            a.persona.nickname = "B"
        with self.assertRaises(ScopeError):
            dataclasses.replace(a, scope=scope())
        self.assertEqual(a.persona.nickname, "A")

    def test_no_global_cache_fallback(self):
        a, b = scope(), scope()
        cache = ScopeCache(2)
        cache.put(a, "same-key", b"group-a", 60)
        self.assertIsNone(cache.get(b, "same-key"))
        with self.assertRaises(ScopeError):
            cache.get(None, "same-key")
        cache.put(b, "same-key", b"group-b", 60)
        cache.delete_scope(a)
        self.assertEqual(cache.get(b, "same-key"), b"group-b")
        self.assertIsNone(cache.get(a, "same-key"))

    def test_empty_bridge_auth_and_unmodified_maibot_fail_closed(self):
        self.assertFalse(authenticate("", ""))
        self.assertFalse(authenticate("x" * 40, "y" * 40))
        self.assertTrue(authenticate("x" * 40, "x" * 40))
        with self.assertRaises(RuntimeError):
            CURRENT.require_ready()


class SchedulerTests(unittest.IsolatedAsyncioTestCase):
    async def test_account_rebinding_does_not_parallelize_the_same_group(self):
        release, started = asyncio.Event(), asyncio.Event()
        running, high = 0, 0
        delivered = []

        async def allow(_):
            return True

        async def generate(c):
            nonlocal running, high
            running += 1
            high = max(high, running)
            started.set()
            try:
                await release.wait()
                return Reply(c.request_id, c.persona.nickname)
            finally:
                running -= 1

        async def deliver(c, _):
            delivered.append(c.request_id)

        a = scope()
        b = dataclasses.replace(a, account_id=str(uuid4()), bot_id="new-bot")
        dispatcher = Dispatcher(generate, allow, deliver)
        runner = asyncio.create_task(dispatcher.run())
        dispatcher.submit(candidate(a))
        await asyncio.wait_for(started.wait(), 1)
        dispatcher.submit(candidate(b))
        await asyncio.sleep(0.01)
        release.set()
        await wait_until(lambda: len(delivered) == 2)
        self.assertEqual(high, 1)
        self.assertEqual(len(delivered), 2)
        await dispatcher.close()
        await runner

    async def test_four_global_one_per_group_and_latest_candidate(self):
        release = asyncio.Event()
        seen, delivered, running_groups = [], [], set()
        high = 0

        async def allow(_):
            return True

        async def generate(c):
            nonlocal high
            self.assertNotIn(c.scope.key, running_groups)
            running_groups.add(c.scope.key)
            high = max(high, len(running_groups))
            seen.append(c.request_id)
            try:
                await release.wait()
                return Reply(c.request_id, c.persona.nickname)
            finally:
                running_groups.remove(c.scope.key)

        async def deliver(c, reply):
            delivered.append((c.request_id, reply.text))

        dispatcher = Dispatcher(generate, allow, deliver)
        runner = asyncio.create_task(dispatcher.run())
        self.addAsyncCleanup(dispatcher.close)
        groups = [scope() for _ in range(5)]
        first = candidate(groups[0], "first")
        dispatcher.submit(first)
        for s in groups[1:]:
            dispatcher.submit(candidate(s))
        await wait_until(lambda: len(seen) == 4)
        self.assertEqual(len(seen), 4)
        dropped = candidate(groups[0], "dropped")
        last = candidate(groups[0], "last")
        dispatcher.submit(dropped)
        self.assertEqual(dispatcher.submit(last), "coalesced")
        release.set()
        await wait_until(lambda: len(delivered) == 6)
        self.assertEqual(high, 4)
        self.assertEqual(len(delivered), 6)
        self.assertNotIn(dropped.request_id, seen)
        self.assertIn((first.request_id, "first"), delivered)
        self.assertIn((last.request_id, "last"), delivered)
        await dispatcher.close()
        await runner

    async def test_revoke_while_generating_blocks_delivery(self):
        started, release = asyncio.Event(), asyncio.Event()
        authorized = True
        delivered = []

        async def allow(_):
            return authorized

        async def generate(c):
            started.set()
            await release.wait()
            return Reply(c.request_id, "must not be sent")

        async def deliver(c, reply):
            delivered.append(reply)

        dispatcher = Dispatcher(generate, allow, deliver)
        runner = asyncio.create_task(dispatcher.run())
        dispatcher.submit(candidate(scope()))
        await asyncio.wait_for(started.wait(), 1)
        authorized = False
        release.set()
        await wait_until(lambda: dispatcher.outcomes.get("revoked") == 1)
        self.assertEqual(delivered, [])
        self.assertEqual(dispatcher.outcomes.get("revoked"), 1)
        await dispatcher.close()
        await runner

    async def test_capacity_deadline_cancel_and_wrong_reply(self):
        async def allow(_):
            return True

        async def wrong(c):
            return Reply(str(uuid4()), "wrong request")

        async def deliver(*_):
            self.fail("invalid output was delivered")

        dispatcher = Dispatcher(wrong, allow, deliver)
        for _ in range(30):
            self.assertEqual(dispatcher.submit(candidate(scope())), "enqueued")
        self.assertEqual(dispatcher.submit(candidate(scope())), "capacity_rejected")
        old = candidate(scope(), expires_at=datetime.now(timezone.utc) - timedelta(seconds=1))
        self.assertEqual(dispatcher.submit(old), "expired")
        runner = asyncio.create_task(dispatcher.run())
        await wait_until(lambda: dispatcher.outcomes.get("failed") == 30)
        self.assertEqual(dispatcher.outcomes.get("failed"), 30)
        await dispatcher.close()
        await runner
