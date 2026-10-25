"""Fill the local WebUI login without printing development credentials in tool output."""

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path

root = Path(__file__).resolve().parents[1]
env = dict(line.split("=", 1) for line in (root / ".env").read_text().splitlines() if "=" in line)
username, password = env["BOOTSTRAP_USERNAME"], env["BOOTSTRAP_PASSWORD"]
wrapper = Path.home() / ".codex/skills/playwright/scripts/playwright_cli.sh"
private = root / "var"
private.mkdir(mode=0o700, exist_ok=True)
logs = root / ".playwright-cli"
if logs.exists():
    logs.chmod(0o700)
code = f"""async (page) => {{
  await page.getByRole('textbox', {{name:'用户名', exact:true}}).fill({json.dumps(username)});
  await page.getByRole('textbox', {{name:'密码', exact:true}}).fill({json.dumps(password)});
  await page.getByRole('button', {{name:'登录', exact:true}}).click();
  await page.getByRole('heading', {{name:'工作台', exact:true}}).waitFor();
}}"""
name = None
try:
    with tempfile.NamedTemporaryFile(mode="w", suffix=".js", dir=private, delete=False) as f:
        name = f.name
        f.write(code)
    result = subprocess.run(
        [str(wrapper), "--session", "wsl-console", "run-code", "--filename", name],
        cwd=root, capture_output=True, text=True,
    )
    for value in (result.stdout, result.stderr):
        sys.stdout.write(value.replace(password, "[REDACTED]"))
    raise SystemExit(result.returncode)
finally:
    if name:
        os.unlink(name)
