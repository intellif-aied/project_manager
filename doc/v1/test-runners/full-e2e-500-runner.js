const fs = require("fs");
const { chromium } = require("playwright");

const API_BASE = process.env.AIDA_API_BASE || "http://192.168.14.157:5173/api/v1";
const WEB_BASE = process.env.AIDA_WEB_BASE || "http://192.168.14.157:5173";
const ACCOUNT_DOC = process.env.AIDA_ACCOUNT_DOC_LOCAL || "/tmp/aida-test-accounts.md";
const PREFIX =
  process.env.AIDA_TEST_PREFIX ||
  `FULL-E2E-${new Date().toISOString().replace(/[-:TZ.]/g, "").slice(0, 14)}`;
const REPORT_PATH = `/tmp/full-e2e-500-${PREFIX}.md`;

const ACCOUNT_KEYS = [
  "PM",
  "DIRECTOR",
  "TL_A",
  "TL_B",
  "EMP_A1",
  "EMP_A2",
  "EMP_A3",
  "EMP_A4",
  "EMP_B1",
  "EMP_B2",
  "EMP_B3",
  "EMP_B4",
];

const ROLE_KEYS = {
  "303": "PM",
  "304": "DIRECTOR",
  "305": "TL_A",
  "306": "TL_B",
  "307": "EMP_A1",
  "308": "EMP_A2",
  "309": "EMP_A3",
  "310": "EMP_A4",
  "311": "EMP_B1",
  "312": "EMP_B2",
  "313": "EMP_B3",
  "314": "EMP_B4",
};

function parseAccounts() {
  const text = fs.readFileSync(ACCOUNT_DOC, "utf8");
  const roleByUid = {};
  const assignedRe =
    /^\|\s*(30[3-9]|31[0-4])\s*\|\s*(t\d+)\s*\|\s*([^|]+?)\s*\|\s*(pm|director|team_leader|employee)\s*\|\s*([^|]+?)\s*\|/gm;
  let match;
  while ((match = assignedRe.exec(text))) {
    roleByUid[match[1]] = {
      uid: match[1],
      username: match[2].trim(),
      name: match[3].trim(),
      role: match[4].trim(),
      team: match[5].trim(),
    };
  }

  const tokenRe =
    /^\|\s*(30[3-9]|31[0-4])\s*\|\s*(t\d+)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*12345678\s*\|\s*`([^`]+)`\s*\|/gm;
  const accounts = {};
  while ((match = tokenRe.exec(text))) {
    const uid = match[1];
    const key = ROLE_KEYS[uid];
    accounts[key] = {
      ...(roleByUid[uid] || {}),
      uid,
      username: match[2].trim(),
      name: match[3].trim(),
      email: match[4].trim(),
      token: match[5].trim(),
      key,
    };
  }
  for (const key of ACCOUNT_KEYS) {
    if (!accounts[key]) throw new Error(`missing account ${key} in ${ACCOUNT_DOC}`);
  }
  return accounts;
}

const accounts = parseAccounts();
const results = [];
const artifacts = [];
const fixtures = {
  requirements: [],
  tasks: [],
  teams: {},
};
let seq = 1;

function nextId() {
  return `HC-${String(seq++).padStart(4, "0")}`;
}

function todayISO() {
  return new Date().toISOString().slice(0, 10);
}

function dateDaysAgo(days) {
  const d = new Date();
  d.setUTCDate(d.getUTCDate() - days);
  return d.toISOString().slice(0, 10);
}

function weekStartISO() {
  const d = new Date();
  const day = d.getUTCDay() || 7;
  d.setUTCDate(d.getUTCDate() - day + 1);
  return d.toISOString().slice(0, 10);
}

function redact(value) {
  if (value == null) return "";
  const text = typeof value === "string" ? value : JSON.stringify(value);
  return text.length > 700 ? `${text.slice(0, 700)}...` : text;
}

async function rawRequest(account, method, apiPath, body, extraHeaders = {}) {
  const headers = {
    Accept: "application/json",
    "Content-Type": "application/json",
    ...extraHeaders,
  };
  if (account?.token && headers.Authorization === undefined) headers.Authorization = `Bearer ${account.token}`;
  const res = await fetch(`${API_BASE}${apiPath}`, {
    method,
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await res.text();
  let data = null;
  try {
    data = text ? JSON.parse(text) : null;
  } catch {
    data = text;
  }
  return { status: res.status, data, text };
}

async function check(module, accountKey, method, apiPath, body, predicate, expected, options = {}) {
  const account = accountKey === "ANON" ? null : accounts[accountKey];
  const started = Date.now();
  const id = options.id || nextId();
  try {
    const res = await rawRequest(account, method, apiPath, body, options.headers || {});
    const ok = Boolean(predicate(res));
    results.push({
      id,
      module,
      account: accountKey,
      method,
      path: apiPath,
      ok,
      status: res.status,
      expected,
      durationMs: Date.now() - started,
      response: ok ? "" : redact(res.data),
    });
    return res;
  } catch (error) {
    results.push({
      id,
      module,
      account: accountKey,
      method,
      path: apiPath,
      ok: false,
      status: "ERR",
      expected,
      durationMs: Date.now() - started,
      response: error.stack || error.message,
    });
    return { status: 0, data: null };
  }
}

async function checkCustom(module, accountKey, method, apiPath, body, predicate, expected, headers) {
  return check(module, accountKey, method, apiPath, body, predicate, expected, { headers });
}

const is2xx = (res) => res.status >= 200 && res.status < 300;
const is200 = (res) => res.status === 200;
const is201 = (res) => res.status === 201;
const is400 = (res) => res.status === 400;
const is401 = (res) => res.status === 401;
const is403 = (res) => res.status === 403;
const is404 = (res) => res.status === 404;
const statusIn = (...items) => (res) => items.includes(res.status);
const list200 = (res) => res.status === 200 && Array.isArray(res.data);
const object200 = (res) => res.status === 200 && res.data && typeof res.data === "object";

async function getJSON(accountKey, apiPath) {
  const res = await rawRequest(accounts[accountKey], "GET", apiPath);
  if (!is2xx(res)) throw new Error(`${accountKey} GET ${apiPath} -> ${res.status} ${redact(res.data)}`);
  return res.data;
}

function accountTeamId(accountKey) {
  const team = accounts[accountKey].team;
  if (team === "小组A") return fixtures.teams.teamA.id;
  if (team === "小组B") return fixtures.teams.teamB.id;
  return null;
}

function canViewRequirement(accountKey, req) {
  const acc = accounts[accountKey];
  if (["pm", "director", "admin"].includes(acc.role)) return true;
  const teamId = accountTeamId(accountKey);
  if (teamId && req.teamIds.includes(teamId)) return true;
  if (acc.role === "team_leader" && req.creatorUid === acc.uid) return true;
  return false;
}

function canViewTask(accountKey, task) {
  const acc = accounts[accountKey];
  if (["pm", "director", "admin"].includes(acc.role)) return true;
  const teamId = accountTeamId(accountKey);
  if (teamId && task.teamIds.includes(teamId)) return true;
  if (acc.role === "team_leader" && task.creatorUid === acc.uid) return true;
  return false;
}

function visibleRequirementFor(accountKey) {
  const teamId = accountTeamId(accountKey);
  if (!teamId) return fixtures.requirements[0];
  return fixtures.requirements.find((req) => req.teamIds.includes(teamId)) || fixtures.requirements[0];
}

async function createRequirement(ownerKey, suffix, teamIds) {
  const payload = {
    title: `${PREFIX} ${suffix}`,
    description: "全量高覆盖自动化测试创建需求，可清理",
    priority: "medium",
    deadline: "2099-12-31",
    team_ids: teamIds,
    acceptance_criteria: ["自动化验收标准 1", "自动化验收标准 2"],
  };
  const res = await rawRequest(accounts[ownerKey], "POST", "/requirements", payload);
  if (!is201(res)) throw new Error(`create requirement ${suffix} failed ${res.status} ${redact(res.data)}`);
  const item = {
    ...res.data,
    key: suffix,
    teamIds,
    creatorUid: accounts[ownerKey].uid,
  };
  fixtures.requirements.push(item);
  return item;
}

async function createTask(ownerKey, requirement, suffix, assigneeUid, dependsOnIds = []) {
  const payload = {
    requirement_id: requirement.id,
    title: `${PREFIX} ${suffix}`,
    acceptance_criteria: ["任务验收标准"],
    assignee_id: assigneeUid,
    priority: "medium",
    due_date: "2099-12-31",
    depends_on_ids: dependsOnIds,
  };
  const res = await rawRequest(accounts[ownerKey], "POST", "/tasks", payload);
  if (!is201(res)) throw new Error(`create task ${suffix} failed ${res.status} ${redact(res.data)}`);
  const fresh = await getJSON("PM", `/tasks/${res.data.id}`);
  const item = {
    ...fresh,
    key: suffix,
    teamIds: requirement.teamIds,
    creatorUid: accounts[ownerKey].uid,
  };
  fixtures.tasks.push(item);
  return item;
}

async function createFixture() {
  const teams = await getJSON("PM", "/teams");
  const teamA = teams.find((t) => t.name === "小组A");
  const teamB = teams.find((t) => t.name === "小组B");
  if (!teamA || !teamB) throw new Error("missing 小组A/小组B in /teams");
  fixtures.teams = { teamA, teamB };

  const reqA = await createRequirement("PM", "REQ_A", [teamA.id]);
  const reqB = await createRequirement("PM", "REQ_B", [teamB.id]);
  const reqMulti = await createRequirement("PM", "REQ_MULTI", [teamA.id, teamB.id]);
  const reqTLA = await createRequirement("TL_A", "REQ_TL_A", [teamA.id]);
  const reqTLB = await createRequirement("TL_B", "REQ_TL_B", [teamB.id]);

  const taskA1 = await createTask("PM", reqA, "TASK_A1", "307");
  const taskA2 = await createTask("PM", reqA, "TASK_A2", "308");
  const taskB1 = await createTask("PM", reqB, "TASK_B1", "311");
  const taskB2 = await createTask("PM", reqB, "TASK_B2", "312");
  const taskMultiA = await createTask("PM", reqMulti, "TASK_MULTI_A", "307");
  const taskTLA = await createTask("TL_A", reqTLA, "TASK_TL_A", "307");
  const taskTLB = await createTask("TL_B", reqTLB, "TASK_TL_B", "311");
  await createTask("PM", reqA, "TASK_DEPENDS_ON_A1", "308", [taskA1.id]);

  return { reqA, reqB, reqMulti, reqTLA, reqTLB, taskA1, taskA2, taskB1, taskB2, taskMultiA, taskTLA, taskTLB };
}

async function freshRequirement(req) {
  const res = await rawRequest(accounts.PM, "GET", `/requirements/${req.id}`);
  return is2xx(res) ? { ...req, ...res.data } : req;
}

async function freshTask(task) {
  const res = await rawRequest(accounts.PM, "GET", `/tasks/${task.id}`);
  return is2xx(res) ? { ...task, ...res.data } : task;
}

async function cleanup() {
  for (const task of [...fixtures.tasks].reverse()) {
    try {
      const fresh = await freshTask(task);
      const version = fresh.version ?? task.version ?? 1;
      await rawRequest(accounts.PM, "DELETE", `/tasks/${task.id}?base_version=${encodeURIComponent(version)}`);
    } catch {}
  }
  for (const req of [...fixtures.requirements].reverse()) {
    try {
      const fresh = await freshRequirement(req);
      const version = fresh.version ?? req.version ?? 1;
      await rawRequest(accounts.PM, "DELETE", `/requirements/${req.id}?base_version=${encodeURIComponent(version)}`);
    } catch {}
  }
}

async function runAuthAndBaseTests() {
  for (const key of ACCOUNT_KEYS) {
    const acc = accounts[key];
    await check(
      "鉴权",
      key,
      "GET",
      "/auth/me",
      undefined,
      (res) => res.status === 200 && String(res.data?.id ?? res.data?.uid) === acc.uid && res.data?.role === acc.role,
      "200 且 uid/role 与账号文档一致"
    );
  }

  await check("鉴权", "ANON", "GET", "/auth/me", undefined, is401, "缺 token 返回 401");
  await checkCustom("鉴权", "ANON", "GET", "/auth/me", undefined, is401, "非 Bearer 返回 401", {
    Authorization: "Token invalid",
  });
  await checkCustom("鉴权", "ANON", "GET", "/auth/me", undefined, is401, "伪造 token 返回 401", {
    Authorization: "Bearer invalid.token",
  });
  await checkCustom("鉴权", "ANON", "GET", "/auth/me", undefined, is401, "空 Bearer 返回 401", {
    Authorization: "Bearer ",
  });
  await checkCustom("鉴权", "ANON", "GET", "/auth/me", undefined, is401, "篡改签名返回 401", {
    Authorization: `Bearer ${accounts.PM.token.slice(0, -4)}xxxx`,
  });

  for (const key of ACCOUNT_KEYS) {
    await check("基础", key, "GET", "/users", undefined, list200, "用户列表 200 array");
    await check("基础", key, "GET", "/teams", undefined, list200, "团队列表 200 array");
    await check("基础", key, "GET", "/task-assignees", undefined, list200, "负责人列表 200 array");
    await check("基础", key, "GET", "/aihub/users/search?search_key=t", undefined, is403, "非 admin 搜索 AIHub 用户 403");
    await check("基础", key, "PUT", `/admin/users/${accounts[key].uid}/profile`, { app_role: accounts[key].role }, is403, "非 admin 修改用户 403");
  }
}

async function runDashboardAndTokenTests() {
  const today = todayISO();
  const yesterday = dateDaysAgo(1);
  const from = dateDaysAgo(14);
  const weekStart = weekStartISO();
  const endpoints = [
    ["/dashboard/follows", is200, "关注事项 200"],
    ["/dashboard/risks", is200, "风险提示 200"],
    ["/reports/today", object200, "今日日报对象 200"],
    ["/reports/mine?page=1&page_size=5", is200, "我的日报列表 200"],
    [`/reports/weekly/mine/current?week_start=${weekStart}`, statusIn(200, 404), "当前个人周报 200/404"],
    [`/reports/weekly/mine/sources?week_start=${weekStart}`, is200, "个人周报来源 200"],
    ["/tokens?period=today&group_by=model&scope=mine", object200, "今日个人 Token 200"],
    ["/tokens?period=week&group_by=model&scope=mine", object200, "本周个人 Token 200"],
    ["/tokens?period=month&group_by=user&scope=mine", object200, "本月个人 Token 200"],
    [`/tokens?period=range&from=${from}&to=${today}&group_by=model&scope=mine`, object200, "区间 Token 按模型 200"],
    [`/tokens?period=range&from=${from}&to=${today}&group_by=user&scope=mine`, object200, "区间 Token 按用户 200"],
    [`/tokens?period=range&from=${from}&to=${today}&group_by=team`, object200, "区间 Token 按团队 200"],
    [`/tokens?period=range&from=${from}&to=${today}&group_by=requirement`, object200, "区间 Token 按需求 200"],
    [`/tokens?period=range&from=${from}&to=${today}&group_by=task`, object200, "区间 Token 按任务 200"],
    [`/tokens/sessions?from=${from}&to=${today}&scope=mine&page=1&page_size=20`, object200, "个人 Token session 200"],
    [`/tokens/sessions?from=${from}&to=${today}&scope=team&page=1&page_size=10`, object200, "团队 Token session 200"],
    ["/sessions?page=1&page_size=20", object200, "工作记录列表 200"],
    [`/teams/activity?date=${today}`, is200, "今日团队活跃 200"],
    [`/teams/activity?date=${yesterday}`, is200, "昨日团队活跃 200"],
    ["/tokens?period=last3days&group_by=model&scope=mine", is400, "非法 period 返回 400"],
    ["/tokens?period=range&group_by=model&scope=mine", is400, "range 缺 from/to 返回 400"],
  ];

  for (const key of ACCOUNT_KEYS) {
    for (const [apiPath, pred, expected] of endpoints) {
      await check("工作台/Token", key, "GET", apiPath, undefined, pred, expected);
    }
  }
}

async function runRequirementTaskReadTests() {
  const listEndpoints = [];
  for (const req of fixtures.requirements) {
    listEndpoints.push([`/requirements?team_id=${req.teamIds[0]}`, list200, `按 team_id 查询 ${req.key}`]);
  }
  listEndpoints.push(["/requirements", list200, "需求列表"]);
  listEndpoints.push(["/requirements?status=active", list200, "active 需求列表"]);
  listEndpoints.push(["/requirements?status=cancelled", list200, "cancelled 需求列表"]);
  listEndpoints.push(["/tasks", list200, "任务列表"]);
  for (const req of fixtures.requirements) {
    listEndpoints.push([`/tasks?requirement_id=${req.id}`, list200, `按需求查询任务 ${req.key}`]);
  }
  for (const uid of ["307", "308", "311", "312"]) {
    listEndpoints.push([`/tasks?assignee_id=${uid}`, list200, `按负责人查询任务 ${uid}`]);
  }
  listEndpoints.push(["/tasks?status=todo", list200, "todo 任务列表"]);
  listEndpoints.push(["/tasks?status=in_progress", list200, "in_progress 任务列表"]);
  listEndpoints.push(["/tasks?status=done", list200, "done 任务列表"]);

  for (const key of ACCOUNT_KEYS) {
    for (const [apiPath, pred, expected] of listEndpoints) {
      await check("需求/任务列表", key, "GET", apiPath, undefined, pred, expected);
    }
  }

  for (const key of ACCOUNT_KEYS) {
    for (const req of fixtures.requirements) {
      const expectedVisible = canViewRequirement(key, req);
      await check(
        "需求详情权限",
        key,
        "GET",
        `/requirements/${req.id}`,
        undefined,
        expectedVisible ? is200 : is404,
        expectedVisible ? `${req.key} 可见 200` : `${req.key} 不可见 404`
      );
      await check(
        "需求AC权限",
        key,
        "GET",
        `/requirements/${req.id}/ac`,
        undefined,
        expectedVisible ? is200 : is404,
        expectedVisible ? `${req.key} AC 可见 200` : `${req.key} AC 不可见 404`
      );
    }
  }

  for (const key of ACCOUNT_KEYS) {
    for (const task of fixtures.tasks.slice(0, 7)) {
      const expectedVisible = canViewTask(key, task);
      await check(
        "任务详情权限",
        key,
        "GET",
        `/tasks/${task.id}`,
        undefined,
        expectedVisible ? is200 : is404,
        expectedVisible ? `${task.key} 可见 200` : `${task.key} 不可见 404`
      );
    }
  }
}

async function runWritePermissionTests() {
  const { reqA, reqB, reqMulti, taskA1, taskA2, taskB1 } = {
    reqA: fixtures.requirements.find((item) => item.key === "REQ_A"),
    reqB: fixtures.requirements.find((item) => item.key === "REQ_B"),
    reqMulti: fixtures.requirements.find((item) => item.key === "REQ_MULTI"),
    taskA1: fixtures.tasks.find((item) => item.key === "TASK_A1"),
    taskA2: fixtures.tasks.find((item) => item.key === "TASK_A2"),
    taskB1: fixtures.tasks.find((item) => item.key === "TASK_B1"),
  };

  for (const key of ACCOUNT_KEYS) {
    await check("写权限/参数", key, "POST", "/requirements", {}, is400, "缺 title/description 返回 400");
    await check(
      "写权限/参数",
      key,
      "POST",
      "/requirements",
      { title: `${PREFIX} BAD_NO_TEAM`, description: "bad" },
      is400,
      "缺 team_id 返回 400"
    );
  }

  await check(
    "写权限/需求",
    "TL_A",
    "POST",
    "/requirements",
    {
      title: `${PREFIX} TL_A_CROSS_TEAM_FORBIDDEN`,
      description: "cross team forbidden",
      priority: "medium",
      deadline: "2099-12-31",
      team_ids: [fixtures.teams.teamB.id],
    },
    is403,
    "TL_A 跨队创建需求 403"
  );
  await check(
    "写权限/需求",
    "TL_B",
    "POST",
    "/requirements",
    {
      title: `${PREFIX} TL_B_CROSS_TEAM_FORBIDDEN`,
      description: "cross team forbidden",
      priority: "medium",
      deadline: "2099-12-31",
      team_ids: [fixtures.teams.teamA.id],
    },
    is403,
    "TL_B 跨队创建需求 403"
  );
  for (const key of ["EMP_A1", "EMP_A2", "EMP_B1", "EMP_B2"]) {
    await check(
      "写权限/需求",
      key,
      "POST",
      "/requirements",
      {
        title: `${PREFIX} ${key}_CREATE_REQ_FORBIDDEN`,
        description: "employee create req forbidden",
        priority: "medium",
        deadline: "2099-12-31",
        team_ids: [accountTeamId(key)],
      },
      is403,
      `${key} 创建需求 403`
    );
  }

  for (const key of ACCOUNT_KEYS) {
    const fresh = await freshRequirement(reqA);
    const canManage = ["PM", "DIRECTOR", "TL_A"].includes(key);
    const expected = canManage ? is200 : statusIn(403, 404);
    const expectedText = canManage ? "可管理 reqA 200" : "无权限编辑需求 403/404";
    await check(
      "写权限/需求",
      key,
      "PUT",
      `/requirements/${reqA.id}`,
      { title: `${PREFIX} REQ_A ${key} touched`, base_version: fresh.version },
      expected,
      expectedText
    );
  }

  for (const key of ACCOUNT_KEYS) {
    const targetReq = key.includes("_B") ? reqB : reqA;
    const assignee = accounts[key].role === "employee" ? accounts[key].uid : key.includes("_B") ? "311" : "307";
    const shouldCreate =
      ["PM", "DIRECTOR", "TL_A", "TL_B"].includes(key) ||
      (accounts[key].role === "employee" && canViewRequirement(key, targetReq));
    const res = await check(
      "写权限/任务",
      key,
      "POST",
      "/tasks",
      {
        requirement_id: targetReq.id,
        title: `${PREFIX} CREATE_TASK_${key}`,
        acceptance_criteria: ["AC"],
        assignee_id: assignee,
        priority: "medium",
        due_date: "2099-12-31",
      },
      shouldCreate ? is201 : statusIn(403, 404),
      shouldCreate ? `${key} 在权限范围内创建任务 201` : `${key} 无权限创建任务 403/404`
    );
    if (is201(res)) {
      try {
        const fresh = await getJSON("PM", `/tasks/${res.data.id}`);
        fixtures.tasks.push({
          ...fresh,
          key: `CREATE_TASK_${key}`,
          teamIds: targetReq.teamIds,
          creatorUid: accounts[key].uid,
        });
      } catch {}
    }
  }

  for (const key of ACCOUNT_KEYS) {
    const fresh = await freshTask(taskA1);
    const canManage = ["PM", "DIRECTOR", "TL_A", "EMP_A1"].includes(key);
    const expected = canManage ? is200 : key.startsWith("EMP_A") ? is403 : is404;
    await check(
      "写权限/任务",
      key,
      "PUT",
      `/tasks/${taskA1.id}/progress`,
      { progress: 10 + (seq % 80), base_version: fresh.version },
      expected,
      canManage ? "可更新任务进度 200" : key.startsWith("EMP_A") ? "同队非负责人进度 403" : "跨队任务进度 404"
    );
  }

  for (const key of ["PM", "DIRECTOR", "TL_A", "EMP_A1", "EMP_A2", "TL_B", "EMP_B1"]) {
    const fresh = await freshTask(taskA2);
    const canManage = ["PM", "DIRECTOR", "TL_A", "EMP_A2"].includes(key);
    const expected = canManage ? is200 : key.startsWith("EMP_A") ? is403 : is404;
    await check(
      "写权限/任务",
      key,
      "PUT",
      `/tasks/${taskA2.id}/status`,
      { status: "in_progress", base_version: fresh.version },
      expected,
      canManage ? "可更新任务状态 200" : "无权限更新任务状态"
    );
  }

  for (const key of ["PM", "DIRECTOR", "TL_A", "EMP_A1", "TL_B", "EMP_B1"]) {
    const fresh = await freshTask(taskA1);
    await check(
      "写权限/任务",
      key,
      "PUT",
      `/tasks/${taskA1.id}/progress`,
      { progress: 150, base_version: fresh.version },
      is400,
      "非法进度返回 400"
    );
  }

  const depFresh = await freshTask(taskA2);
  await check(
    "写权限/任务",
    "PM",
    "POST",
    `/tasks/${taskA2.id}/dependencies`,
    { depends_on_id: taskA1.id, base_version: depFresh.version },
    is200,
    "PM 增加同需求依赖 200"
  );
  const depAfter = await freshTask(taskA2);
  await check(
    "写权限/任务",
    "EMP_A1",
    "POST",
    `/tasks/${taskA2.id}/dependencies`,
    { depends_on_id: taskB1.id, base_version: depAfter.version },
    is403,
    "非负责人/非法依赖不允许"
  );

  for (const key of ACCOUNT_KEYS) {
    const req = visibleRequirementFor(key);
    await check("关注", key, "POST", "/follows", { target_type: "requirement", target_id: req.id }, is2xx, "关注可见需求 2xx");
    await check("关注", key, "GET", "/dashboard/follows", undefined, is200, "关注后工作台关注列表 200");
    await check("关注", key, "DELETE", `/follows/requirement/${req.id}`, undefined, is2xx, "取消关注 2xx");
  }

  for (const key of ["EMP_A1", "EMP_A2", "TL_A"]) {
    await check(
      "关注",
      key,
      "POST",
      "/follows",
      { target_type: "requirement", target_id: reqB.id },
      statusIn(403, 404),
      "关注跨队不可见需求 403/404"
    );
  }
  for (const key of ["EMP_B1", "EMP_B2", "TL_B"]) {
    await check(
      "关注",
      key,
      "POST",
      "/follows",
      { target_type: "requirement", target_id: reqA.id },
      statusIn(403, 404),
      "关注跨队不可见需求 403/404"
    );
  }
  await check("关注", "PM", "POST", "/follows", { target_type: "requirement", target_id: reqMulti.id }, is2xx, "PM 关注多团队需求 2xx");
  await check("关注", "PM", "DELETE", `/follows/requirement/${reqMulti.id}`, undefined, is2xx, "PM 取消多团队需求关注 2xx");
}

function teamEndpointPredicate(accountKey, apiPath) {
  const role = accounts[accountKey].role;
  if (role === "team_leader") return statusIn(200, 404);
  if (role === "employee") {
    if (apiPath.includes("/reports/team/today")) return statusIn(200, 404);
    return is403;
  }
  return statusIn(200, 400, 403, 404);
}

function departmentEndpointPredicate(accountKey) {
  return accounts[accountKey].role === "director" ? statusIn(200, 404) : is403;
}

async function runReportTests() {
  const today = todayISO();
  const yesterday = dateDaysAgo(1);
  const from = dateDaysAgo(14);
  const weekStart = weekStartISO();
  const personalEndpoints = [
    ["/reports?page=1&page_size=10", is200, "日报列表 200"],
    ["/reports/mine?page=1&page_size=10", is200, "我的日报 200"],
    ["/reports/today", object200, "今日日报 200"],
    [`/reports?from=${from}&to=${today}`, is200, "日报区间查询 200"],
    [`/reports?date=${today}`, is200, "日报按日期查询 200"],
    [`/reports?date=${yesterday}`, is200, "日报按昨日查询 200"],
    [`/reports/weekly/mine?page=1&page_size=10`, is200, "个人周报历史 200"],
    [`/reports/weekly/mine/current?week_start=${weekStart}`, statusIn(200, 404), "当前个人周报 200/404"],
    [`/reports/weekly/mine/sources?week_start=${weekStart}`, is200, "个人周报来源 200"],
    [`/reports/weekly/mine/current/generate`, is400, "个人周报空来源生成 400"],
  ];

  for (const key of ACCOUNT_KEYS) {
    for (const [apiPath, pred, expected] of personalEndpoints) {
      if (apiPath.endsWith("/generate")) {
        await check(
          "日报/周报个人",
          key,
          "POST",
          `${apiPath}?week_start=${weekStart}`,
          { week_start: weekStart, source_daily_report_ids: [] },
          pred,
          expected
        );
      } else {
        await check("日报/周报个人", key, "GET", apiPath, undefined, pred, expected);
      }
    }
  }

  const teamEndpoints = [
    [`/reports/team/members?date=${today}`, "小组成员日报"],
    [`/reports/team/sources?date=${today}`, "小组日报来源"],
    [`/reports/team/today?date=${today}`, "当前小组日报"],
    [`/reports/team?date=${today}`, "小组日报历史"],
    [`/reports/team/weekly/sources?week_start=${weekStart}`, "小组周报来源"],
    [`/reports/team/weekly/current?week_start=${weekStart}`, "当前小组周报"],
    [`/reports/team/weekly?week_start=${weekStart}`, "小组周报历史"],
  ];
  for (const key of ACCOUNT_KEYS) {
    for (const [apiPath, label] of teamEndpoints) {
      await check("日报/周报小组", key, "GET", apiPath, undefined, teamEndpointPredicate(key, apiPath), `${label} 按角色返回`);
    }
  }

  const departmentEndpoints = [
    [`/reports/department/sources?date=${today}`, "部门日报来源"],
    [`/reports/department/today?date=${today}`, "当前部门日报"],
    [`/reports/department?date=${today}`, "部门日报历史"],
    [`/reports/department/weekly/sources?week_start=${weekStart}`, "部门周报来源"],
    [`/reports/department/weekly/current?week_start=${weekStart}`, "当前部门周报"],
    [`/reports/department/weekly?week_start=${weekStart}`, "部门周报历史"],
  ];
  for (const key of ACCOUNT_KEYS) {
    for (const [apiPath, label] of departmentEndpoints) {
      await check("日报/周报部门", key, "GET", apiPath, undefined, departmentEndpointPredicate(key), `${label} 按角色返回`);
    }
  }
}

async function runUiTests() {
  const browser = await chromium.launch({ headless: true });
  const pages = [
    ["/dashboard", ["工作台"], "UI 工作台"],
    ["/requirements", ["需求看板"], "UI 需求看板"],
    ["/reports/daily", ["日报"], "UI 日报"],
    ["/reports/weekly", ["周报"], "UI 周报"],
    ["/tokens", ["Token"], "UI Token"],
  ];

  for (const role of ACCOUNT_KEYS) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, deviceScaleFactor: 1 });
    await context.addInitScript((token) => localStorage.setItem("token", token), accounts[role].token);
    const page = await context.newPage();
    for (const [route, words, module] of pages) {
      const started = Date.now();
      const id = nextId();
      const screenshot = `/tmp/aida-full-e2e-500-${PREFIX}-${role}-${route.replace(/[^a-z0-9]/gi, "_")}.png`;
      try {
        await page.goto(`${WEB_BASE}${route}`, { waitUntil: "domcontentloaded", timeout: 30000 });
        await page.waitForTimeout(500);
        const body = await page.locator("body").innerText({ timeout: 6000 });
        const hasWords = words.every((word) => body.includes(word));
        const noOldCopy = !/Agent \/ MCP|发送状态不在当前服务统计|发送进度未纳入当前视图/.test(body);
        const noOverflow = await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth);
        if (!hasWords || !noOldCopy || !noOverflow) {
          await page.screenshot({ path: screenshot, fullPage: true });
          artifacts.push(screenshot);
        }
        results.push({
          id,
          module,
          account: role,
          method: "GET",
          path: route,
          ok: hasWords && noOldCopy && noOverflow,
          status: "PAGE",
          expected: `包含 ${words.join("/")}；无旧技术文案；无横向溢出`,
          durationMs: Date.now() - started,
          response: hasWords ? "" : `body missing expected words; screenshot=${screenshot}`,
        });
      } catch (error) {
        try {
          await page.screenshot({ path: screenshot, fullPage: true });
          artifacts.push(screenshot);
        } catch {}
        results.push({
          id,
          module,
          account: role,
          method: "GET",
          path: route,
          ok: false,
          status: "ERR",
          expected: "页面可打开",
          durationMs: Date.now() - started,
          response: error.stack || error.message,
        });
      }
    }
    await context.close();
  }

  const context = await browser.newContext({ viewport: { width: 1366, height: 900 }, deviceScaleFactor: 1 });
  const page = await context.newPage();
  const started = Date.now();
  const id = nextId();
  try {
    await page.goto(`${WEB_BASE}/dashboard`, { waitUntil: "domcontentloaded", timeout: 30000 });
    await page.waitForTimeout(500);
    const url = page.url();
    const body = await page.locator("body").innerText({ timeout: 5000 }).catch(() => "");
    const ok = url.includes("/login") || /登录|登陆|login/i.test(body);
    results.push({
      id,
      module: "UI 鉴权",
      account: "ANON",
      method: "GET",
      path: "/dashboard",
      ok,
      status: "PAGE",
      expected: "未登录访问跳转登录或展示登录态",
      durationMs: Date.now() - started,
      response: ok ? "" : `url=${url}; body=${body.slice(0, 200)}`,
    });
  } catch (error) {
    results.push({
      id,
      module: "UI 鉴权",
      account: "ANON",
      method: "GET",
      path: "/dashboard",
      ok: false,
      status: "ERR",
      expected: "未登录访问跳转登录或展示登录态",
      durationMs: Date.now() - started,
      response: error.stack || error.message,
    });
  }
  await context.close();
  await browser.close();
}

function writeReport() {
  const passed = results.filter((r) => r.ok).length;
  const failed = results.length - passed;
  const byModule = {};
  for (const result of results) {
    byModule[result.module] ||= { total: 0, passed: 0, failed: 0 };
    byModule[result.module].total += 1;
    if (result.ok) byModule[result.module].passed += 1;
    else byModule[result.module].failed += 1;
  }

  const lines = [];
  lines.push("# Aida 高覆盖全量测试执行报告");
  lines.push("");
  lines.push(`执行时间：${new Date().toISOString()}`);
  lines.push(`前缀：\`${PREFIX}\``);
  lines.push(`API_BASE：\`${API_BASE}\``);
  lines.push(`WEB_BASE：\`${WEB_BASE}\``);
  lines.push("");
  lines.push("## 汇总");
  lines.push("");
  lines.push("| 总数 | 通过 | 失败 |");
  lines.push("| ---: | ---: | ---: |");
  lines.push(`| ${results.length} | ${passed} | ${failed} |`);
  lines.push("");
  lines.push("## 分模块");
  lines.push("");
  lines.push("| 模块 | 总数 | 通过 | 失败 |");
  lines.push("| --- | ---: | ---: | ---: |");
  for (const [module, item] of Object.entries(byModule)) {
    lines.push(`| ${module} | ${item.total} | ${item.passed} | ${item.failed} |`);
  }
  lines.push("");
  lines.push("## 失败明细");
  lines.push("");
  lines.push("| ID | 模块 | 账号 | 请求 | 状态 | 期望 | 响应摘要 |");
  lines.push("| --- | --- | --- | --- | --- | --- | --- |");
  for (const r of results.filter((item) => !item.ok)) {
    lines.push(
      `| ${r.id} | ${r.module} | ${r.account} | ${r.method} ${r.path} | ${r.status} | ${String(r.expected).replaceAll("|", "/")} | ${String(r.response).replaceAll("|", "/")} |`
    );
  }
  if (failed === 0) lines.push("| - | - | - | - | - | - | - |");
  lines.push("");
  lines.push("## 全量明细");
  lines.push("");
  lines.push("| ID | 模块 | 账号 | 请求 | 状态 | 结果 | 耗时(ms) |");
  lines.push("| --- | --- | --- | --- | --- | --- | ---: |");
  for (const r of results) {
    lines.push(`| ${r.id} | ${r.module} | ${r.account} | ${r.method} ${r.path} | ${r.status} | ${r.ok ? "PASS" : "FAIL"} | ${r.durationMs} |`);
  }
  lines.push("");
  lines.push("## UI 失败截图");
  lines.push("");
  if (artifacts.length === 0) lines.push("- 无失败截图。");
  for (const artifact of artifacts) lines.push(`- \`${artifact}\``);
  fs.writeFileSync(REPORT_PATH, lines.join("\n"), "utf8");
  console.log(JSON.stringify({ report: REPORT_PATH, total: results.length, passed, failed }, null, 2));
}

(async () => {
  try {
    await runAuthAndBaseTests();
    await createFixture();
    await runDashboardAndTokenTests();
    await runRequirementTaskReadTests();
    await runWritePermissionTests();
    await runReportTests();
    await runUiTests();
  } finally {
    await cleanup();
    writeReport();
  }
})().catch((error) => {
  results.push({
    id: "RUNNER",
    module: "执行器",
    account: "-",
    method: "-",
    path: "-",
    ok: false,
    status: "ERR",
    expected: "runner completes",
    durationMs: 0,
    response: error.stack || error.message,
  });
  writeReport();
  process.exitCode = 1;
});
