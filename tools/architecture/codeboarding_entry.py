"""Pinned CLI adapter: cap outputs without patching the installed distribution."""
from agents.llm_config import LLM_PROVIDERS
from codeboarding_cli.main import main

LLM_PROVIDERS["openai"].extra_args["max_tokens"] = 4096
LLM_PROVIDERS["openai"].extra_args["max_retries"] = 0
main()
