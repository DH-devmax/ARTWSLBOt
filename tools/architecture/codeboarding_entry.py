"""Pinned CLI adapter: cap outputs without patching the installed distribution."""
import sys


def configure_chat_transport(provider):
    provider.extra_args.update(max_tokens=4096, max_retries=0, use_responses_api=False)


def configure_single_attempt(*modules):
    for module in modules:
        module.LLM_MAX_ATTEMPTS = 1


def forbid_full_fallback(generator):
    def blocked(*_args, **_kwargs):
        raise ValueError("Full analysis fallback is disabled; explicit initialization required")
    generator.generate_analysis = blocked


def main():
    from agents import scope_analysis_agent, tree_planner_agent
    from agents.llm_config import LLM_PROVIDERS
    from codeboarding_cli.main import main as upstream_main
    from diagram_analysis.diagram_generator import DiagramGenerator

    configure_chat_transport(LLM_PROVIDERS["openai"])
    configure_single_attempt(scope_analysis_agent, tree_planner_agent)
    if sys.argv[1:2] == ["incremental"]:
        forbid_full_fallback(DiagramGenerator)
    upstream_main()


if __name__ == "__main__":
    main()
