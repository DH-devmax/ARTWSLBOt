// Synthetic website authentication only; no business credentials or native calls.
// prettier-ignore
async (page) => {
  const captcha = await process.getBuiltinModule('vm').runInThisContext(
    `(${process.getBuiltinModule('fs').readFileSync('tools/web_captcha_ui_smoke.js', 'utf8')})`,
    { filename: 'web_captcha_ui_smoke.js' }
  )(page, { fixtureOnly: true })
  const base = "http://127.0.0.1:5173";
  const user = {
    id: "ae000000-0000-4000-8000-000000000097",
    username: "synthetic",
    role: "superadmin",
  };
  const permissions = ["overview:read", "accounts:read", "groups:read"];
  const saved = await page.evaluate(() => ({
    session: { ...sessionStorage },
    theme: localStorage.getItem("sys-theme"),
  }));
  let loggedIn = false,
    fail = true,
    posts = 0,
    resolveLogin,
    loginRequested;
  const pattern = "**/api/v1/**";
  const handler = async (route) => {
    if (await captcha.handle(route)) return;
    const req = route.request(),
      path = new URL(req.url()).pathname.replace("/api/v1", "");
    captcha.consume(req);
    let data = [];
    if (req.method() !== "GET") {
      if (path === "/auth/login") {
        posts++;
        const body = req.postDataJSON();
        if (
          body.username !== "synthetic" ||
          body.password !== "fixture-password"
        )
          throw new Error("Login input binding failed");
        if (fail)
          return route.fulfill({
            status: 401,
            json: { code: 1, msg: "invalid_credentials" },
          });
        loginRequested();
        await new Promise((resolve) => {
          resolveLogin = resolve;
        });
        loggedIn = true;
        data = { user };
      } else if (path === "/auth/logout") loggedIn = false;
      else throw new Error("Theme caused unrelated write: " + path);
    } else if (path === "/me") {
      if (!loggedIn)
        return route.fulfill({
          status: 401,
          json: { code: 1, msg: "unauthorized" },
        });
      data = { user, permissions };
    } else if (path === "/workspaces")
      data = {
        items: [
          { id: "wsl", name: "旺商聊机器人" },
          { id: "lajiaoliao", name: "辣椒聊机器人" },
        ],
      };
    else if (path === "/overview")
      data = {
        accounts: 0,
        groups: 0,
        ai_groups: 0,
        limits: { accounts: 10, groups: 300, ai_groups: 300 },
        native_ready: false,
        ai_ready: false,
      };
    await route.fulfill({ json: { code: 0, msg: "", data } });
  };
  const fits = async (name, width, height = 900) => {
    await page.setViewportSize({ width, height });
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    if (
      !(await page.evaluate(
        () => document.documentElement.scrollWidth <= innerWidth + 1,
      ))
    )
      throw new Error(`${name} overflow at ${width}`);
    if (
      !(await page
        .locator("img")
        .evaluateAll((images) =>
          images.every((img) => img.complete && img.naturalWidth),
        ))
    )
      throw new Error("Product asset missing");
    await page.screenshot({
      path: `output/playwright/theme-${name}-${width}.png`,
      fullPage: true,
      animations: "disabled",
    });
  };
  await page.route(pattern, handler);
  try {
    await page.evaluate(() => {
      sessionStorage.clear();
      localStorage.setItem("sys-theme", "dark");
    });
    await page.goto(base);
    await page.reload();
    await page
      .getByRole("heading", { name: "登录工作空间", exact: true })
      .waitFor();
    const username = page.getByRole("textbox", { name: "用户名", exact: true });
    const password = page.getByLabel("密码", { exact: true });
    const submit = page.getByRole("button", { name: "登录", exact: true });
    if (!(await submit.isDisabled())) throw new Error("Empty login enabled");
    if (await page.locator("html.dark").count())
      throw new Error("Demo theme affected product");
    for (const width of [320, 390, 768, 1440]) await fits("login", width, 780);
    await username.focus();
    await page.keyboard.press("Tab");
    if (!(await password.evaluate((el) => el === document.activeElement)))
      throw new Error("Login keyboard order failed");
    await username.fill("synthetic");
    await password.fill("fixture-password");
    await password.press("Enter");
    await captcha.complete();
    await page.locator(".login-form .el-alert").waitFor();
    if (posts !== 1 || !(await submit.isEnabled()))
      throw new Error("Failed login cannot be retried");
    await fits("login-error", 390);
    fail = false;
    const requested = new Promise((resolve) => {
      loginRequested = resolve;
    });
    await password.press("Enter");
    await captcha.complete();
    await requested;
    if (!(await username.isDisabled()) || !(await password.isDisabled()))
      throw new Error("Busy login input editable");
    await page.locator(".login-form form").evaluate((form) => {
      form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
      form.dispatchEvent(
        new Event("submit", { bubbles: true, cancelable: true }),
      );
    });
    if (posts !== 2) throw new Error("Repeated login submitted");
    resolveLogin();
    await page.locator(".platform-shell").waitFor();
    const expected = await page.evaluate(() => ({
      accent: getComputedStyle(document.documentElement)
        .getPropertyValue("--ui-lime")
        .trim(),
      canvas: getComputedStyle(document.querySelector(".main-area")).backgroundColor,
      sidebar: getComputedStyle(document.querySelector(".sidebar"))
        .backgroundColor,
      spacing: getComputedStyle(document.querySelector(".brand-name"))
        .letterSpacing,
    }));
    if (
      expected.accent !== "#d8ff36" ||
      expected.canvas !== "rgb(238, 239, 241)" ||
      expected.sidebar !== "rgb(255, 255, 255)" ||
      !["normal", "0px"].includes(expected.spacing)
    )
      throw new Error("Product theme not applied");
    await page.setViewportSize({ width: 1440, height: 900 });
    const mainWidth = await page.locator(".main-area").evaluate(el => el.clientWidth);
    await page.getByRole("button", { name: "折叠侧栏", exact: true }).click();
    if (await page.locator(".main-area").evaluate(el => el.clientWidth) <= mainWidth)
      throw new Error("Collapsed sidebar did not release workspace width");
    const groupNav = page.locator(".sidebar nav").getByRole("button", { name: "群组管理", exact: true });
    if (!(await groupNav.isVisible())) throw new Error("Collapsed navigation lost page controls");
    await groupNav.click();
    await fits("collapsed-groups", 1440);
    await page.reload();
    await page.locator(".page-heading h1").filter({hasText:"群组管理"}).waitFor();
    if (new URL(page.url()).hash !== "#groups")
      throw new Error("Session restoration replaced the requested page route");
    await page.getByRole("button", { name: "折叠侧栏", exact: true }).click();
    await page.getByRole("button", { name: "展开侧栏", exact: true }).click();
    const section = page.locator(".nav-group").filter({
      has: page.locator('.nav-link[aria-label="群组管理"]'),
    });
    const sectionToggle = section.locator(".nav-group-title button");
    await sectionToggle.click();
    if (await groupNav.isVisible()) throw new Error("Navigation section did not collapse");
    await sectionToggle.press("Enter");
    if (!(await groupNav.isVisible())) throw new Error("Navigation section did not reopen with keyboard");
    const beforeSkip = new URL(page.url()).hash;
    await page.locator(".skip-navigation").focus();
    await page.keyboard.press("Enter");
    if (!(await page.locator("#workspace-content").evaluate(el => el === document.activeElement))
        || new URL(page.url()).hash !== beforeSkip)
      throw new Error("Skip navigation changed the selected business page");
    await page.goto(base + "/#overview");
    await page.getByRole("button", { name: "查看验收状态", exact: true }).click();
    const acceptance = page.locator(".acceptance-popover").filter({ visible: true });
    await acceptance.getByText("原生接入", { exact: true }).waitFor();
    if (await acceptance.getByText("未验收", { exact: true }).count() !== 2)
      throw new Error("Readiness presentation changed the acceptance state");
    await page.getByRole("button", { name: "查看验收状态", exact: true }).click();
    await acceptance.waitFor({ state: "hidden" });
    for (const width of [320, 390, 768, 1440]) {
      await fits("overview", width);
      if (width <= 760) {
        await page
          .getByRole("button", { name: "展开导航", exact: true })
          .click();
        await page.waitForFunction(
          () =>
            document.querySelector(".sidebar").getBoundingClientRect().left >=
            0,
        );
        await fits("navigation", width);
        await page.locator('.sidebar').getByRole('button', { name: '收起导航', exact: true }).focus();
        await page.keyboard.press('Escape');
        if (await page.getByRole('button', { name: '展开导航', exact: true }).getAttribute('aria-expanded') !== 'false')
          throw new Error('Mobile navigation did not close with Escape');
        await page.getByRole('button', { name: '展开导航', exact: true }).click();
        await page
          .locator(".sidebar")
          .getByRole("button", { name: "群组管理", exact: true })
          .click();
        await page.waitForFunction(
          () =>
            document.querySelector(".sidebar").getBoundingClientRect().right <=
            1,
        );
        await page.goto(base + "/#overview");
      }
    }
    await page.getByRole("button", { name: "切换工作台", exact: true }).click();
    await page
      .getByRole("dialog", { name: "选择平台", exact: true })
      .getByRole("button", { name: "辣椒聊机器人", exact: true })
      .click();
    await page.locator(".page-heading").getByText("辣椒聊机器人", { exact: false }).waitFor();
    await fits("lajiaoliao", 1440);
    await page.getByRole("button", { name: "用户菜单", exact: true }).hover();
    await page.getByRole("menuitem", { name: "退出登录", exact: true }).click();
    await page
      .getByRole("heading", { name: "登录工作空间", exact: true })
      .waitFor();
    if (await password.inputValue())
      throw new Error("Password retained after logout");
    if (
      await page.evaluate(() =>
        JSON.stringify({ ...sessionStorage, ...localStorage }).includes(
          "fixture-password",
        ),
      )
    )
      throw new Error("Password persisted in browser");
    return {
      passed: true,
      synthetic_only: true,
      login_posts: posts,
      duplicate_submit_blocked: true,
      keyboard: true,
      demo_theme_isolated: true,
      desktop_collapse: true,
      keyboard_group_navigation: true,
      skip_preserves_route: true,
      viewports: [320, 390, 768, 1440],
      native_writes: 0,
      model_calls: 0,
    };
  } finally {
    resolveLogin?.();
    await page.unroute(pattern, handler);
    await page.evaluate((values) => {
      sessionStorage.clear();
      for (const [key, value] of Object.entries(values.session))
        sessionStorage.setItem(key, value);
      if (values.theme === null) localStorage.removeItem("sys-theme");
      else localStorage.setItem("sys-theme", values.theme);
    }, saved);
  }
}
