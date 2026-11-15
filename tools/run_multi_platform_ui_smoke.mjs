// Development-only browser verification of built assets, with all APIs mocked.
import fs from "node:fs/promises";
import path from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const assets = path.resolve(process.env.WSL_UI_ASSET_ROOT || path.join(root, "web/dist"));
const checks = process.argv.slice(2);
const additionalChecks = [
  "theme_ui_smoke.js",
  "console_ui_smoke.js",
  "account_dashboard_ui_smoke.js",
  "native_receiver_ui_smoke.js",
  "runtime_monitor_ui_smoke.js",
  "heat_chat_ui_smoke.js",
  "gateway_inspection_ui_smoke.js",
  "platform_assistant_ui_smoke.js",
  "go_console_ui_smoke.js",
  "business_modules_ui_smoke.js",
  "native_product_scope_ui_smoke.js",
  "assistant_dual_platform_ui_smoke.js",
  "assistant_conversations_ui_smoke.js",
  "assistant_outcomes_ui_smoke.js",
  "assistant_rendering_ui_smoke.js",
  "assistant_composer_ui_smoke.js",
  "assistant_ownership_ui_smoke.js",
  "assistant_modules_ui_smoke.js",
  "customer_onboarding_ui_smoke.js",
  "operator_demo_ui_smoke.js",
  "group_reproof_ui_smoke.js",
  "operator_demo_claim_ui_smoke.js",
  "customer_package_ui_smoke.js",
  "user_center_empty_ui_smoke.js",
  "assistant_binding_ui_smoke.js",
  "demo_friend_ui_smoke.js",
  "onboarding_incoming_ui_smoke.js",
  "onboarding_controlled_ui_smoke.js",
  "controlled_onboarding_entry_ui_smoke.js",
  "friend_delete_ui_smoke.js",
  "friend_approve_ui_smoke.js",
  "onboarding_friend_gate_ui_smoke.js",
  "mobile_search_auth_ui_smoke.js",
  "web_captcha_ui_smoke.js",
];
if (!checks.length) checks.push("multi_platform_ui_smoke.js");
if (
  checks.some(
    (name) =>
      ![
        ...additionalChecks,
        "group_membership_ui_smoke.js",
        "chili_self_invitation_ui_smoke.js",
        "lajiaoliao_auth_ui_smoke.js",
        "media_sending_ui_smoke.js",
        "lajiaoliao_media_ui_smoke.js",
        "commercial_ui_smoke.js",
        "assistant_ui_smoke.js",
        "multi_platform_ui_smoke.js",
        "messages_ui_smoke.js",
        "message_media_ui_smoke.js",
        "message_video_ui_smoke.js",
        "live_updates_ui_smoke.js",
        "message_sending_ui_smoke.js",
        "native_members_ui_smoke.js",
        "native_settings_ui_smoke.js",
        "native_social_ui_smoke.js",
        "native_auth_error_ui_smoke.js",
        "native_acceptance_ui_smoke.js",
        "test_tools_ui_smoke.js",
        "native_roles_ui_smoke.js",
        "lajiaoliao_native_ui_smoke.js",
      ].includes(name),
  )
)
  throw new Error("Unsupported synthetic browser check");
const { chromium } = await import(
  process.env.WSL_PLAYWRIGHT_MODULE || "playwright"
);
await fs.mkdir(path.join(root, "output/playwright"), { recursive: true });
process.chdir(root);
const browser = await chromium.launch({
  headless: true,
  channel: process.env.WSL_BROWSER_CHANNEL || "chrome",
  ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
    ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH }
    : {}),
});
try {
  for (const name of checks) {
    const page = await browser.newPage();
    const errors = [];
    page.on("pageerror", (error) => errors.push(error.stack || error.message));
    await page.route("**/*", async (route) => {
      const url = new URL(route.request().url());
      if (url.origin !== "http://127.0.0.1:5173")
        return route.abort("blockedbyclient");
      if (url.pathname.startsWith("/api/"))
        return route.fulfill({
          status: 500,
          json: { code: 1, msg: "unmocked_api" },
        });
      const target = path.resolve(
        assets,
        "." + (url.pathname === "/" ? "/index.html" : url.pathname),
      );
      if (!target.startsWith(assets + path.sep))
        return route.abort("blockedbyclient");
      try {
        await route.fulfill({
          body: await fs.readFile(target),
          contentType:
            {
              ".js": "text/javascript",
              ".css": "text/css",
              ".html": "text/html",
              ".png": "image/png",
              ".svg": "image/svg+xml",
            }[path.extname(target)] || "application/octet-stream",
        });
      } catch {
        await route.fulfill({ status: 404, body: "missing_asset" });
      }
    });
    await page.goto("http://127.0.0.1:5173");
    const source = await fs.readFile(path.join(root, "tools", name), "utf8");
    const run = vm.runInThisContext(`(${source})`, { filename: name });
    const fixtureMe = {
      user: { id: "ae000000-0000-4000-8000-000000000001", role: "superadmin" },
      permissions: [
        "overview:read",
        "accounts:read",
        "groups:read",
        "operations:read",
        "workspaces:maibot",
      ],
    };
    let result;
    try {
      result = await run(page, fixtureMe);
      if (errors.length)
        throw new Error(`Browser runtime errors: ${errors.join("; ")}`);
      console.log(JSON.stringify({ check: name, ...result }));
    } catch (error) {
      await page.screenshot({
        path: `output/playwright/${name}-failure.png`,
        fullPage: true,
      });
      await fs.writeFile(
        `output/playwright/${name}-failure.txt`,
        await page.locator("body").innerText(),
      );
      throw error;
    } finally {
      await page.close();
    }
  }
} finally {
  await browser.close();
}
