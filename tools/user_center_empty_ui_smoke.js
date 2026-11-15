// Built product assets and synthetic API responses only; never forwards native calls.
// prettier-ignore
async (page) => {
  page.setDefaultTimeout(10000);
  const base = "http://127.0.0.1:5173";
  const id = (n) => `e6000000-0000-4000-8000-${String(n).padStart(12, "0")}`;
  let mode = "owner",
    active = true,
    platforms = ["wsl", "lajiaoliao"];
  let claimsAllowed = true,
    populated = false,
    failGroups = false,
    deferGroups = false;
  let releaseGroups, groupDeferred;
  const reads = [],
    writes = [],
    screenshots = [];
  const pendingRequests = new Set();
  const resetURL = `${base}/__user-center-empty-fixture`;
  const reset = (route) =>
    route.fulfill({ contentType: "text/html", body: "<!doctype html><title>Fixture reset</title>" });
  const actor = () => ({
    id: id(1),
    username: "synthetic-empty-state",
    role: mode === "demo" ? "superadmin" : "user",
    ...(mode !== "demo"
      ? {
          tenant_id: id(2),
          tenant_role: mode === "member" ? "viewer" : "owner",
        }
      : {}),
  });
  const permissions = () => [
    "overview:read",
    "groups:read",
    "claims:read",
    "operations:read",
    ...(claimsAllowed ? ["claims:create"] : []),
    ...(mode === "demo" ? ["accounts:read", "users:manage"] : []),
  ];
  const space = () => ({ id: mode === "demo" ? id(90) : id(2),
    name: mode === "demo" ? "我的演示工作空间" : "合成客户",
    kind: mode === "demo" ? "operator_demo" : "tenant_business", owner_id: id(1) });
  const plan = () => mode === "demo"
    ? { id: "operator_unlimited", name: "超管专属 · 不限额", system_only: true, active: true,
      unlimited: true, permanent: true, platforms: [], group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null }
    : { id: id(91), name: "PRO", active, system_only: false, unlimited: false, permanent: false,
      platforms, modules: ["management_assistant"], group_limit: 3, member_limit: 10, assistant_limit: 100, credit_limit: 1000000 };
  const respond = async (route) => {
    const req = route.request(),
      url = new URL(req.url()),
      name = url.pathname.replace("/api/v1", "");
    if (req.method() !== "GET") {
      writes.push({ name, method: req.method() });
      return route.fulfill({
        status: 500,
        json: { code: 1, msg: "synthetic_write_not_allowed" },
      });
    }
    let data = [];
    if (name === "/me") data = { user: actor(), permissions: permissions() };
    else if (name === "/workspaces")
      data = {
        items: [
          { id: "wsl", name: "旺商聊机器人" },
          { id: "lajiaoliao", name: "辣椒聊机器人" },
        ],
      };
    else if (name === "/user-center")
      data = {
        workspace: space(), effective_plan: plan(),
        billing_source: mode === "demo" ? "platform_demo" : "tenant_wallet",
        quotas: { groups: plan().group_limit, members: plan().member_limit, assistant: plan().assistant_limit, credits: plan().credit_limit },
        subscription:
          mode === "demo"
            ? null
            : {
                name: "PRO",
                active,
                group_limit: 3,
                member_limit: 10,
                platforms,
                modules: ["management_assistant"],
                expires_at: "2026-12-06T00:00:00Z",
              },
        entitlement: {
          mode: mode === "demo" ? "operator_demo" : "tenant_business",
          name: mode === "demo" ? "演示测试套餐" : "PRO",
          unlimited: mode === "demo",
          permissions: permissions(),
        },
        groups: [],
        claims: [],
        operations: populated
          ? Array.from({ length: 9 }, (_, index) => ({
              id: id(10 + index),
              group_id: id(3),
              kind: "send_text",
              status: "succeeded",
              source: "controlled_test",
              created_at: `2026-11-06T03:${String(59 - index).padStart(2, "0")}:00Z`,
            }))
          : [],
        personas: [],
      };
    else if (name === "/user-center/groups") {
      const selected = url.searchParams.get("platform");
      if (
        selected !== req.headers()["x-workspace"] ||
        url.searchParams.get("page_size") !== "20" ||
        (mode === "demo" && !["mine", "all"].includes(url.searchParams.get("workspace")))
      )
        throw Error("Empty group query lost platform or pagination: " + JSON.stringify({
          mode, platform: selected, header: req.headers()["x-workspace"],
          page_size: url.searchParams.get("page_size"), workspace: url.searchParams.get("workspace"),
        }));
      reads.push({
        platform: selected,
        search: url.searchParams.get("search"),
        customer: url.searchParams.get("tenant_id"),
      });
      if (deferGroups) {
        deferGroups = false;
        groupDeferred?.();
        await new Promise((resolve) => {
          releaseGroups = resolve;
        });
      }
      if (failGroups)
        return route.fulfill({
          status: 503,
          json: { code: 1, msg: "synthetic_group_read_failed" },
        });
      const items =
        populated && !url.searchParams.get("search")
          ? [
              {
                group_id: id(3),
                name: "已有授权群",
                platform: selected,
                active: true,
                tenant_id: space().id,
                workspace: space(),
                robot_nickname: "原生机器人",
                primary_account_id: id(4),
              },
            ]
          : [];
      data = {
        workspace: space(), effective_plan: plan(),
        items,
        total: items.length,
        page: 1,
        page_size: 20,
        customers: [],
      };
    } else if (name === "/user-center/usage")
      data = { call_count: 0, sampled_at: new Date().toISOString() };
    else if (name === "/overview")
      data = { accounts: 0, groups: 0, limits: { accounts: 10, groups: 300 } };
    else if (name === "/group-binding/context")
      data = { mode: mode === "demo" ? "operator_demo" : "tenant_business", workspace: space(),
        default_target_tenant_id: space().id, can_onboard_for_customer: mode === "demo",
        customers: mode === "demo" ? [{ tenant_id: id(2), name: "合成客户" }] : [], next_offset: -1 };
    await route.fulfill({ json: { code: 0, msg: "", data } });
  };
  const handler = async (route) => {
    const pending = respond(route);
    pendingRequests.add(pending);
    try {
      await pending;
    } finally {
      pendingRequests.delete(pending);
    }
  };
  const guide = () =>
    page.getByRole("region", { name: "群组接入指引", exact: true });
  const list = () => page.locator(".uc-mobile-groups:visible, .uc-groups:visible");
  const navigate = async (platform = "wsl", pageName = "groups", configure = () => {}) => {
    // Retire the previous document and finish its requests before changing its actor fixture.
    await page.goto(resetURL);
    await Promise.all([...pendingRequests]);
    configure();
    await page.evaluate(
      ({ actorID, platform }) => {
        sessionStorage.clear();
        sessionStorage.setItem(`wsl:console:${actorID}`, "user");
        sessionStorage.setItem(`wsl:workspace:${actorID}`, platform);
      },
      { actorID: actor().id, platform },
    );
    await page.goto(`${base}/#${pageName}`);
    await page.locator(".user-center").waitFor();
  };
  const fit = async (label, width) => {
    await page.setViewportSize({ width, height: 820 });
    await page.evaluate(
      () =>
        new Promise((resolve) =>
          requestAnimationFrame(() => requestAnimationFrame(resolve)),
        ),
    );
    if (
      await page.evaluate(
        () => document.documentElement.scrollWidth > innerWidth + 1,
      )
    )
      throw Error(`Empty state overflow ${label}/${width}`);
    const button = guide().getByRole("button").first();
    if (await button.count()) {
      const box = await button.boundingBox();
      if (!box || box.x < 0 || box.x + box.width > width + 1)
        throw Error(`Empty state action clipped ${label}/${width}`);
    }
    const file = `output/playwright/user-empty-${label}-${width}.png`;
    await page.screenshot({
      path: file,
      fullPage: true,
      animations: "disabled",
    });
    screenshots.push(file);
  };
  await page.route("**/api/v1/**", handler);
  await page.route(resetURL, reset);
  try {
    for (const platform of ["wsl", "lajiaoliao"]) {
      await navigate(platform);
      await guide()
        .getByRole("heading", {
          name: `接入你的第一个${platform === "wsl" ? "旺商聊" : "辣椒聊"}群组`,
        })
        .waitFor();
      await page.waitForFunction(
        () =>
          document.querySelectorAll('[aria-label="群组接入指引"] li').length ===
          3,
      );
      if (await page.locator(".uc-groups,.uc-mobile-groups").count())
        throw Error("Empty table duplicated onboarding");
      for (const width of [320, 390, 768, 1440]) {
        await fit(platform, width);
        await guide()
          .getByRole("button", { name: "接入第一个群组", exact: true })
          .click();
        const modal = page.getByRole("dialog", {
          name: "接入我的群组",
          exact: true,
        });
        await modal.waitFor();
        const selected = modal.getByRole("radio", {
          name: platform === "wsl" ? "旺商聊" : "辣椒聊",
          exact: true,
        });
        if (!(await selected.isChecked()))
          throw Error("Onboarding opened the wrong platform");
        const start = await modal
          .getByRole("button", { name: "开始核验", exact: true })
          .boundingBox();
        if (
          !start ||
          start.x < 0 ||
          start.x + start.width > width + 1 ||
          start.y < 0 ||
          start.y + start.height > 821
        )
          throw Error("Onboarding action outside viewport");
        await modal.getByRole("button", { name: "关闭", exact: true }).click();
        await modal.waitFor({ state: "hidden" });
        if (writes.length)
          throw Error("Opening or closing onboarding submitted a request");
      }
    }
    await navigate("wsl", "groups", () => { mode = "member"; });
    await guide()
      .getByText(
        "请联系客户负责人，将对应群组分配给你。完成授权后会显示在这里。",
        { exact: true },
      )
      .waitFor();
    if (
      (await guide().getByRole("button").count()) ||
      (await guide().locator("li").count())
    )
      throw Error("Unassigned member was offered a binding action");
    await fit("member", 390);
    await navigate("wsl", "groups", () => {
      mode = "owner";
      active = false;
    });
    await guide()
      .getByText("请联系平台管理员开通包含旺商聊的套餐，再接入自己的群组。", {
        exact: true,
      })
      .waitFor();
    if (
      await page
        .getByRole("button", { name: /^(接入群组|接入第一个群组)$/ })
        .count()
    )
      throw Error("Inactive package exposed onboarding");
    await navigate("wsl", "groups", () => {
      active = true;
      claimsAllowed = false;
    });
    await guide()
      .getByText("当前账号没有群接入权限，请联系平台管理员。", { exact: true })
      .waitFor();
    if (
      await page
        .getByRole("button", { name: /^(接入群组|接入第一个群组)$/ })
        .count()
    )
      throw Error("Missing claim permission exposed onboarding");
    await navigate("lajiaoliao", "groups", () => {
      claimsAllowed = true;
      platforms = ["wsl"];
    });
    await guide()
      .getByText(/当前套餐未包含辣椒聊/)
      .waitFor();
    await guide()
      .getByRole("button", { name: "切换到旺商聊", exact: true })
      .click();
    await guide()
      .getByRole("heading", { name: "接入你的第一个旺商聊群组", exact: true })
      .waitFor();
    await navigate("wsl", "groups", () => {
      platforms = ["wsl", "lajiaoliao"];
      mode = "demo";
    });
    await guide()
      .getByRole("button", { name: "接入第一个群组", exact: true })
      .waitFor();
    await fit("demo", 1440);
    await guide()
      .getByRole("button", { name: "接入第一个群组", exact: true })
      .click();
    const demoModal = page.getByRole("dialog", { name: "接入群组", exact: true });
    await demoModal.getByRole("radio", { name: "我的工作空间", exact: true }).waitFor();
    if (!await demoModal.getByRole("radio", { name: "我的工作空间", exact: true }).isChecked() ||
      await demoModal.getByRole("combobox", { name: "群组所属客户", exact: true }).count())
      throw Error("Demo onboarding did not default to own workspace");
    await demoModal.locator("label.el-radio").filter({ hasText: "代客户接入" }).click();
    await demoModal.getByRole("combobox", { name: "群组所属客户", exact: true }).waitFor();
    if (!await demoModal.getByRole("button", { name: "开始核验", exact: true }).isDisabled())
      throw Error("Demo onboarding inferred its customer");
    if (await page.evaluate(() => location.hash !== "#groups"))
      throw Error("Demo onboarding left the user center");
    await demoModal.getByRole("button", { name: "关闭", exact: true }).click();
    await demoModal.waitFor({ state: "hidden" });
    const mark = page.locator("#primary-sidebar .brand .admin-mark");
    if ((await mark.getAttribute("viewBox")) !== "360 380 1320 1240")
      throw Error("Sidebar SVG uses an approximated viewBox");
    const expected = [
      "928 781 1021 951 784.5 1371.97 1618 1371.97 1530.32 1544 509 1539 928 781",
      "1618 1371.97 784.5 1371.97 874.93 1211 1346 1211 923.1 456 1110.06 456 1618 1371.97",
      "418 1372.74 509 1539 928 781 1162.32 1211 1346 1211 923.1 456 418 1372.74",
    ];
    const points = await mark
      .locator("polygon")
      .evaluateAll((nodes) => nodes.map((el) => el.getAttribute("points")));
    if (JSON.stringify(points) !== JSON.stringify(expected))
      throw Error("Sidebar mark geometry differs from reference");
    await page.emulateMedia({ reducedMotion: "no-preference" });
    const motion = await mark.evaluate((el) => {
      const animations = el.getAnimations({ subtree: true });
      for (const animation of animations) {
        animation.pause();
        animation.currentTime = 0;
      }
      const first = [...el.children].map((item) => getComputedStyle(item).fill);
      for (const animation of animations) animation.currentTime = 1360;
      const next = [...el.children].map((item) => getComputedStyle(item).fill);
      return { count: animations.length, first, next };
    });
    if (
      motion.count !== 4 ||
      JSON.stringify(motion.first) === JSON.stringify(motion.next)
    )
      throw Error("Sidebar mark float or color cycle missing");
    await page.screenshot({
      path: "output/playwright/user-empty-reference-logo-1440.png",
      animations: "disabled",
    });
    await page.emulateMedia({ reducedMotion: "reduce" });
    if (
      !(await mark.evaluate((el) =>
        [el, ...el.children].every(
          (item) => getComputedStyle(item).animationName === "none",
        ),
      ))
    )
      throw Error("Sidebar logo ignored reduced motion");
    for (const platform of ["wsl", "lajiaoliao"]) {
      await navigate(platform, "overview", () => {
        mode = "owner";
        populated = true;
      });
      const start = page.getByRole("region", {
        name: "工作台接入指引",
        exact: true,
      });
      await start
        .getByRole("button", { name: "接入新群组", exact: true })
        .waitFor();
      await list().getByText("已有授权群", { exact: true }).waitFor();
      if (await page.locator(".uc-package,.uc-usage").count())
        throw Error("Workbench still starts with billing details");
      const activity = page.getByRole("region", {
        name: "最近操作",
        exact: true,
      });
      if ((await activity.locator("li").count()) !== 5)
        throw Error("Workbench did not limit recent activity to five records");
      if ((await activity.getByText("受控测试", { exact: true }).count()) !== 5)
        throw Error("Workbench exposes an internal operation source label");
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 820 });
        const primary = start.getByRole("button", {
          name: "接入新群组",
          exact: true,
        });
        const box = await primary.boundingBox();
        if (
          !box ||
          box.x < 0 ||
          box.x + box.width > width + 1 ||
          box.y < 0 ||
          box.y + box.height > 820
        )
          throw Error(
            "Workbench onboarding is not actionable in the first viewport: " +
              width,
          );
        if (
          await page.evaluate(
            () => document.documentElement.scrollWidth > innerWidth + 1,
          )
        )
          throw Error("Workbench overflow at " + width);
        if (
          (await page
            .locator("#primary-sidebar .brand .admin-mark polygon")
            .count()) !== 3
        )
          throw Error("User center SVG missing");
        if (width <= 1000) {
          const columns = await page.locator(".uc-summary").evaluate(
            (el) => getComputedStyle(el).gridTemplateColumns.split(" ").length,
          );
          if (columns !== 2)
            throw Error("Narrow workbench metrics are not arranged in two columns");
          if (!(await page.locator(".uc-mobile-groups").isVisible()))
            throw Error("Narrow workbench still hides actions inside a wide table");
          if (!(await page.locator(".uc-mobile-groups")
            .getByRole("button", { name: "群功能", exact: true }).isVisible()))
            throw Error("Narrow workbench group action is not directly visible");
        }
        const file = `output/playwright/user-workbench-${platform}-${width}.png`;
        await page.screenshot({
          path: file,
          fullPage: true,
          animations: "disabled",
        });
        screenshots.push(file);
        await primary.click();
        const modal = page.getByRole("dialog", {
          name: "接入我的群组",
          exact: true,
        });
        await modal.waitFor();
        if (
          !(await modal
            .getByRole("radio", {
              name: platform === "wsl" ? "旺商聊" : "辣椒聊",
              exact: true,
            })
            .isChecked())
        )
          throw Error("Workbench did not pass its selected platform");
        await modal.getByRole("button", { name: "关闭", exact: true }).click();
        await modal.waitFor({ state: "hidden" });
      }
      await activity.getByRole("button", { name: "查看全部", exact: true }).click();
      await page.waitForURL("**/#operations");
      await page.getByRole("tab", { name: "操作记录", exact: true }).click();
      if ((await page.locator(".uc-records .el-table__body-wrapper tbody tr").count()) !== 9)
        throw Error("Full activity records were lost after leaving the overview");
      await navigate(platform, "overview");
      await start
        .getByRole("button", { name: "查看授权群", exact: true })
        .click();
      await page.waitForURL("**/#groups");
      if (await start.count())
        throw Error("Workbench guidance leaked into a different page");
    }
    await navigate("wsl", "overview", () => { mode = "member"; });
    if (
      await page
        .getByRole("button", { name: /^(接入新群组|绑定群组)$/ })
        .count()
    )
      throw Error("Member gained workbench onboarding");
    await navigate("wsl", "overview", () => { mode = "demo"; });
    const demoStart = page.getByRole("region", {
      name: "工作台接入指引",
      exact: true,
    });
    await demoStart
      .getByRole("button", { name: "绑定群组", exact: true })
      .waitFor();
    if (
      await demoStart
        .getByRole("button", { name: "接入新群组", exact: true })
        .count()
    )
      throw Error("Demo workbench requires a commercial subscription");
    await demoStart
      .getByRole("button", { name: "绑定群组", exact: true })
      .click();
    const workbenchModal = page.getByRole("dialog", { name: "接入群组", exact: true });
    await workbenchModal.getByRole("radio", { name: "我的工作空间", exact: true }).waitFor();
    if (!await workbenchModal.getByRole("radio", { name: "我的工作空间", exact: true }).isChecked() ||
      await workbenchModal.getByRole("combobox", { name: "群组所属客户", exact: true }).count())
      throw Error("Workbench onboarding did not default to own workspace");
    if (await page.evaluate(() => location.hash !== "#overview"))
      throw Error("Workbench onboarding redirected to admin");
    await workbenchModal.getByRole("button", { name: "关闭", exact: true }).click();
    await workbenchModal.waitFor({ state: "hidden" });
    await navigate("wsl", "groups", () => { mode = "owner"; });
    await list().getByText("已有授权群", { exact: true }).waitFor();
    await page
      .getByRole("textbox", { name: "搜索我的群组", exact: true })
      .fill("没有匹配项");
    await guide()
      .getByRole("heading", { name: "没有找到匹配的群组", exact: true })
      .waitFor();
    if (
      await guide()
        .getByRole("button", { name: "接入第一个群组", exact: true })
        .count()
    )
      throw Error("Search miss was presented as first-time onboarding");
    await guide()
      .getByRole("button", { name: "清除筛选", exact: true })
      .click();
    await list().getByText("已有授权群", { exact: true }).waitFor();
    if (await guide().count())
      throw Error("Cleared search kept empty-state guidance");
    await navigate("wsl", "groups", () => {
      populated = false;
      failGroups = true;
    });
    await page
      .locator(".user-center .el-alert")
      .filter({ hasText: "synthetic_group_read_failed" })
      .waitFor();
    if (await guide().count())
      throw Error("Failed group read masqueraded as no groups");
    await navigate("wsl", "groups", () => { failGroups = false; });
    await guide()
      .getByRole("heading", { name: "接入你的第一个旺商聊群组", exact: true })
      .waitFor();
    await page.waitForFunction(
      () => document.querySelector('button[aria-label="刷新用户中心"]')?.disabled === false,
    );
    deferGroups = true;
    const deferred = new Promise((resolve) => {
      groupDeferred = resolve;
    });
    await page
      .getByRole("button", { name: "刷新用户中心", exact: true })
      .click();
    await deferred;
    await guide().waitFor({ state: "hidden" });
    if (await guide().count())
      throw Error("Pending group read exposed premature onboarding");
    releaseGroups();
    releaseGroups = undefined;
    await guide()
      .getByRole("heading", { name: "接入你的第一个旺商聊群组", exact: true })
      .waitFor();
    if (writes.length) throw Error("Empty-state UI performed a mutation");
    return {
      passed: true,
      synthetic_only: true,
      platforms: ["wsl", "lajiaoliao"],
      roles: ["owner", "member", "superadmin"],
      viewports: [320, 390, 768, 1440],
      platform_preserved: true,
      readonly_on_open_close: true,
      filters: true,
      package_and_permissions: true,
      loading_and_errors: true,
      reference_svg: true,
      reduced_motion: true,
      workbench_first_viewport: true,
      populated_group_guidance: true,
      bounded_recent_activity: true,
      writes: writes.length,
      native_calls: 0,
      model_calls: 0,
      group_queries: reads.length,
      screenshots,
    };
  } finally {
    releaseGroups?.();
    await Promise.all([...pendingRequests]);
    await page.unroute("**/api/v1/**", handler);
    await page.unroute(resetURL, reset);
  }
}
