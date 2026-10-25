from dataclasses import dataclass

BASELINE = "062d45cf8bd4272ac57d7ec03fcf405e5a458211"
REQUIRED_COMPONENTS = frozenset({
    "persona_snapshot", "chat_history", "person_impressions", "expressions",
    "jargon", "summaries", "vector_graph", "media_memory", "plugin_tools",
    "model_call_gate", "cache_scope", "retention",
})


@dataclass(frozen=True, slots=True)
class MaiCompatibility:
    commit: str
    verified_components: frozenset[str]

    def require_ready(self) -> None:
        if self.commit != BASELINE or self.verified_components != REQUIRED_COMPONENTS:
            raise RuntimeError("MaiBot tenant isolation is not release-verified")


# No native MaiBot core patch has been accepted yet. Do not expose its legacy API.
CURRENT = MaiCompatibility(BASELINE, frozenset())
