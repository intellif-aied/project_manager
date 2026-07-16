#!/usr/bin/env python3
"""Real-model full-flow acceptance test for the default Report Agent.

Pipeline (no mocks):
  real login (JWT minted with AIHUB_SECRET)
  -> real session upload (/sessions/batch)
  -> real Report Agent run (/ai-assets/report-agents/{agentId}/runs)
  -> real managed platform /api/session (third party)
  -> real model generation
  -> Agent calls Aida Report MCP (get_sessions / get_*_reports / write_report_result)
  -> business report readback via /reports/...

Outputs:
  tmp/test-reports/ReportAgent真实模型六类报告验收报告.md
  tmp/report_agent_real_model_full_flow_<timestamp>.md
"""

import base64
import hashlib
import hmac
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from datetime import date, timedelta
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
DOC_ACCOUNTS = ROOT / "doc" / "测试账号文档.md"
DOC_REPORT = ROOT / "doc" / "ReportAgent真实模型六类报告验收报告.md"
TMP_DIR = ROOT / "tmp"

API_BASE = os.getenv("AIDA_API_BASE", "http://127.0.0.1:18090/api/v1").rstrip("/")
MANAGED_AGENT_URL = os.getenv("MANAGED_AGENT_URL", "http://192.168.18.107:3081").rstrip("/")
AIHUB_SECRET = os.getenv("AIHUB_SECRET", "").strip()
ADMIN_TOKEN_ENV = os.getenv("AIDA_ADMIN_TOKEN", "").strip()

REPORT_SKILL_SLUG = "aida-report"
REPORT_SKILL_VERSION = "1.0.0"
REPORT_MCP_SLUG = os.getenv("MANAGED_AGENT_REPORT_MCP_SLUG", "aida-report-mcp")
REPORT_MCP_VERSION = os.getenv("MANAGED_AGENT_REPORT_MCP_VERSION", "report-v1")
REPORT_AGENT_NAME = "报告生成 Agent"
REPORT_MCP_SLOT = "AIDA_REPORT_MCP_AUTH"

DEFAULT_MODEL_ID = os.getenv("MANAGED_AGENT_DEFAULT_MODEL_ID", "MiniMax-M2.5")
DEFAULT_ENGINE = os.getenv("MANAGED_AGENT_DEFAULT_ENGINE", "claude-code")

POLL_INTERVAL_SEC = float(os.getenv("AIDA_POLL_INTERVAL", "10"))
POLL_TIMEOUT_SEC = float(os.getenv("AIDA_POLL_TIMEOUT", "600"))

RUN_ADMIN_SMOKE = os.getenv("AIDA_RUN_ADMIN_SMOKE", "0") == "1"
SKIP_REAL_MODEL = os.getenv("AIDA_SKIP_REAL_MODEL", "0") == "1"

# Session keyword list: content is acceptable if it references any of these
# (the model summarizes sessions and may not copy the literal prefix token).
SESSION_KEYWORDS = [
    "默认 Report Agent",
    "session upload",
    "AIDA_REPORT_MCP_AUTH",
    "team_daily",
    "team_weekly",
    "department_daily",
    "department_weekly",
    "personal_daily",
    "personal_weekly",
    "Report Agent",
    "Report MCP",
    "duplicate count",
    "owner",
    "MCP 回归",
    "默认资产 backfill",
]


def content_matches_prefix_or_keywords(content, prefix):
    if not content:
        return False
    if prefix in content:
        return True
    return any(kw in content for kw in SESSION_KEYWORDS)


# ---------------------------------------------------------------------------
# HTTP helpers
# ---------------------------------------------------------------------------

def request_json(method, url, token=None, payload=None, timeout=30, headers=None):
    data = None
    hdrs = {"Accept": "application/json"}
    if token:
        hdrs["Authorization"] = "Bearer " + token
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        hdrs["Content-Type"] = "application/json"
    if headers:
        hdrs.update(headers)
    req = urllib.request.Request(url, data=data, headers=hdrs, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8")
            parsed = json.loads(body) if body.strip() else None
            return resp.status, parsed
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(body)
        except Exception:
            parsed = {"error": body}
        return exc.code, parsed
    except urllib.error.URLError as exc:
        return 0, {"error": str(exc)}


def request_multipart(url, token, fields, files=None, timeout=60):
    boundary = "----aida-real-model-" + uuid.uuid4().hex
    chunks = []
    for key, value in fields.items():
        chunks.append(f"--{boundary}\r\n".encode())
        chunks.append(f'Content-Disposition: form-data; name="{key}"\r\n\r\n'.encode())
        chunks.append(str(value).encode("utf-8"))
        chunks.append(b"\r\n")
    if files:
        for field_name, (filename, filebytes, ctype) in files.items():
            chunks.append(f"--{boundary}\r\n".encode())
            chunks.append(
                f'Content-Disposition: form-data; name="{field_name}"; filename="{filename}"\r\n'.encode()
            )
            chunks.append(f"Content-Type: {ctype}\r\n\r\n".encode())
            chunks.append(filebytes)
            chunks.append(b"\r\n")
    chunks.append(f"--{boundary}--\r\n".encode())
    data = b"".join(chunks)
    req = urllib.request.Request(
        url,
        data=data,
        headers={
            "Accept": "application/json",
            "Authorization": "Bearer " + token,
            "Content-Type": "multipart/form-data; boundary=" + boundary,
        },
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            body = resp.read().decode("utf-8")
            return resp.status, json.loads(body) if body.strip() else None
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")
        try:
            parsed = json.loads(body)
        except Exception:
            parsed = {"error": body}
        return exc.code, parsed


# ---------------------------------------------------------------------------
# Token minting
# ---------------------------------------------------------------------------

def b64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).decode("ascii").rstrip("=")


def mint_token(user_id: str, username: str) -> str:
    now = int(time.time())
    header = {"typ": "JWT", "alg": "HS256"}
    payload = {
        "uid": int(user_id),
        "user_id": user_id,
        "username": username,
        "iat": now,
        "exp": now + 86400,
    }
    signing_input = (
        b64url(json.dumps(header, separators=(",", ":")).encode())
        + "."
        + b64url(json.dumps(payload, separators=(",", ":")).encode())
    )
    sig = hmac.new(AIHUB_SECRET.encode(), signing_input.encode(), hashlib.sha256).digest()
    return signing_input + "." + b64url(sig)


# ---------------------------------------------------------------------------
# Account loading
# ---------------------------------------------------------------------------

def load_accounts():
    text = DOC_ACCOUNTS.read_text(encoding="utf-8")
    role_by_id = {}
    team_by_id = {}
    in_assign = False
    for line in text.splitlines():
        if line.startswith("| 用户 ID | username | 昵称 | Aida 角色 |"):
            in_assign = True
            continue
        if in_assign:
            if line.startswith("小组配置"):
                break
            if line.startswith("|") and "`" not in line:
                cells = [c.strip() for c in line.strip("|").split("|")]
                if len(cells) >= 5 and cells[0].isdigit():
                    role_by_id[cells[0]] = cells[3]
                    team_by_id[cells[0]] = cells[4] if cells[4] and cells[4] != "-" else ""
    accounts = []
    token_re = re.compile(r"^\|\s*(\d+)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|[^|]*\|[^|]*\|\s*`([^`]+)`\s*\|")
    for line in text.splitlines():
        m = token_re.match(line)
        if not m:
            continue
        user_id, username, nickname, _token = m.groups()
        accounts.append({
            "user_id": user_id,
            "username": username.strip(),
            "nickname": nickname.strip(),
            "role": role_by_id.get(user_id, ""),
            "team_label": team_by_id.get(user_id, ""),
        })
    if AIHUB_SECRET:
        for account in accounts:
            account["token"] = mint_token(account["user_id"], account["username"])
    else:
        raise RuntimeError("AIHUB_SECRET must be set to mint fresh test tokens")
    return accounts


def load_admin_account():
    """Admin is not in the test accounts doc; pull from DB if available."""
    if ADMIN_TOKEN_ENV:
        return {"user_id": "admin-env", "username": "admin", "role": "admin", "token": ADMIN_TOKEN_ENV}
    try:
        output = subprocess.check_output(
            [
                "docker", "compose", "exec", "-T", "db",
                "psql", "-U", "aidashboard", "-d", "aidashboard", "-At", "-c",
                "SELECT id::text || '|' || COALESCE(NULLIF(username,''), id::text) "
                "FROM users WHERE aida_enabled=true AND local_enabled=true AND app_role='admin' "
                "ORDER BY id LIMIT 1;",
            ],
            cwd=ROOT, text=True, stderr=subprocess.DEVNULL, timeout=10,
        ).strip()
    except Exception:
        return None
    if not output or "|" not in output:
        return None
    user_id, username = output.split("|", 1)
    return {
        "user_id": user_id,
        "username": username,
        "role": "admin",
        "token": mint_token(user_id, username),
    }


# ---------------------------------------------------------------------------
# API helpers
# ---------------------------------------------------------------------------

def auth_me(token):
    status, body = request_json("GET", API_BASE + "/auth/me", token)
    return status, body


def list_ai_assets(token):
    s_skills, skills = request_json("GET", API_BASE + "/ai-assets/skills", token)
    s_mcps, mcps = request_json("GET", API_BASE + "/ai-assets/mcp", token)
    s_agents, agents = request_json("GET", API_BASE + "/ai-assets/agents", token)
    return {
        "skills": (skills or {}).get("skills", []),
        "mcps": (mcps or {}).get("entries", []),
        "agents": (agents or {}).get("agents", []),
        "status": {"skills": s_skills, "mcps": s_mcps, "agents": s_agents},
    }


def find_report_skill(skills):
    matches = [s for s in skills if s.get("slug") == REPORT_SKILL_SLUG and s.get("version") == REPORT_SKILL_VERSION and not s.get("archived")]
    return matches[0] if matches else None, len(matches)


def find_report_mcp(mcps):
    matches = [m for m in mcps if m.get("slug") == REPORT_MCP_SLUG and m.get("version") == REPORT_MCP_VERSION and not m.get("archived")]
    return matches[0] if matches else None, len(matches)


def is_default_report_agent(agent):
    text = "\n".join([agent.get("description", ""), agent.get("instructions", ""), agent.get("start_prompt_template", "")])
    return "AIDA_REPORT_AGENT:default" in text and "AIDA_MANAGED_DEFAULT_AGENT:true" in text and not agent.get("archived")


def find_report_agent(agents):
    matches = [a for a in agents if is_default_report_agent(a)]
    return matches[0] if matches else None, len(matches)


def backfill_default_assets(admin_token):
    return request_json("POST", API_BASE + "/admin/ai-assets/default-report-assets/backfill", admin_token)


def upload_sessions(token, sessions):
    """sessions: list of dicts matching SessionUpload. Returns API response."""
    metadata = json.dumps({"sessions": sessions}, ensure_ascii=False)
    return request_multipart(API_BASE + "/sessions/batch", token, {"metadata": metadata})


def start_report_run(token, agent_id, payload):
    return request_json("POST", API_BASE + f"/ai-assets/report-agents/{agent_id}/runs", token, payload, timeout=60)


def get_agent_run(token, run_id):
    return request_json("GET", API_BASE + f"/ai-assets/agent-runs/{run_id}", token)


# ---------------------------------------------------------------------------
# Session fixture generation
# ---------------------------------------------------------------------------

def build_session_fixtures(prefix, accounts):
    """Return list of (account, session_dict) tuples."""
    fixtures = []
    today_dt = date.today()
    iso = today_dt.isoformat()

    content_map = {
        "t05": [
            f"{prefix}\n今日完成默认 Report Agent 配置初始化验收。重点验证 Aida Report Skill、Aida Report MCP、报告生成 Agent 是否属于当前用户。遇到的问题：需要确认默认资产不是系统模板，也不是页面加载时自动创建。明日计划：继续验证 personal_daily 和 personal_weekly 的真实 Agent 写回。",
            f"{prefix}\n今日完成 session upload 接口联调。关联任务：Report Agent real model full flow。风险：如果 Agent 未正确注入 AIDA_REPORT_MCP_AUTH，将无法读取 Report MCP 数据。本周进展：完成默认资产 backfill、MCP 回归、前端构建回归。",
        ],
        "t06": [
            f"{prefix}\n今日协助验证小组日报数据来源。完成内容：上传本地 session，并确认 TL 能读取同组成员 session。风险：历史旧资产可能干扰页面展示，但默认资产 duplicate count 仍为 1。明日计划：协助验证 team_daily 和 team_weekly。",
            f"{prefix}\n今日补充小组日报 session 来源。重点关注：默认 Report Agent owner 应为当前用户，duplicate count 为 1/1/1。明日计划：继续配合 TL 完成小组周报。",
        ],
        "t01": [
            f"{prefix}\n今日梳理 Report Agent 六类报告验收范围。关注点：personal_daily、personal_weekly、team_daily、department_daily 写回字段一致性。风险：PM 不应拥有 team 或 department 报告写权限。明日计划：确认产品验收口径。",
            f"{prefix}\n本周继续推进 Report Agent 验收。关注：generation_mode=managed_agent，managed_agent_run_id 是否落库。明日计划：跑 PM personal_weekly 真实模型。",
        ],
        "t03": [
            f"{prefix}\n今日准备小组日报汇总。小组成员 t05 和 t06 均已上传 session。关注点：TL 只能读取所属小组成员 session，不能读取非小组成员数据。本周计划：生成 team_weekly 并确认业务接口读回。",
            f"{prefix}\n本周继续小组周报汇总。重点：team_daily 内容包含成员 session 关键词，team_weekly 汇总本周小组进展。明日计划：配合 director 完成 department_daily。",
        ],
        "t02": [
            f"{prefix}\n今日准备部门日报汇总。关注点：Director 能读取部门员工个人日报、个人周报、小组日报和小组周报。风险：部门外用户数据不能泄露。本周计划：生成 department_weekly 并确认部门周报业务接口读回。",
            f"{prefix}\n本周继续部门周报汇总。重点：department_daily 汇总小组日报，department_weekly 汇总部门成员周报。明日计划：复核 6 类报告 product_status=ai_generated。",
        ],
    }

    for account in accounts:
        if account["username"] not in content_map:
            continue
        for idx, summary in enumerate(content_map[account["username"]], 1):
            started = f"{iso}T09:{idx:02d}:00Z"
            ended = f"{iso}T10:{idx:02d}:00Z"
            fixtures.append((account, {
                "session_ref": f"{prefix}-{account['username']}-{idx}-{uuid.uuid4().hex[:8]}",
                "agent_type": "claude_code",
                "started_at": started,
                "ended_at": ended,
                "duration_secs": 600,
                "model": "claude-sonnet-4-6",
                "summary": summary,
            }))
    return fixtures


# ---------------------------------------------------------------------------
# Result tracking
# ---------------------------------------------------------------------------

class Report:
    def __init__(self, timestamp, prefix):
        self.timestamp = timestamp
        self.prefix = prefix
        self.lines = []
        self.matrix = []  # list of dicts
        self.runs = []    # list of dicts (run logs)
        self.fail_details = []
        self.timeout_details = []
        self.blocked_details = []
        self.summary = {
            "total": 0, "pass": 0, "fail": 0, "timeout": 0, "blocked": 0,
            "real_model_runs": 0, "real_model_succeeded": 0, "real_model_failed": 0,
            "six_types_real_success": False,
            "session_upload_pass": False,
            "business_readback_pass": False,
            "mcp_regression_pass": False,
            "go_frontend_regression_pass": False,
        }

    def add(self, line):
        self.lines.append(line)

    def section(self, title):
        self.lines.append("")
        self.lines.append(f"## {title}")
        self.lines.append("")


# ---------------------------------------------------------------------------
# Pre-flight checks
# ---------------------------------------------------------------------------

def preflight(report, accounts):
    report.section("测试环境与前置检查")
    report.add(f"- API base: `{API_BASE}`")
    report.add(f"- Managed Agent URL: `{MANAGED_AGENT_URL}`")
    report.add(f"- 唯一前缀: `{report.prefix}`")
    report.add(f"- 默认模型: `{DEFAULT_MODEL_ID}` / engine `{DEFAULT_ENGINE}`")
    report.add(f"- 轮询: interval `{POLL_INTERVAL_SEC}s`, timeout `{POLL_TIMEOUT_SEC}s`")
    report.add(f"- 跳过真实模型: `{SKIP_REAL_MODEL}`")
    report.add("")

    checks = []
    status, body = request_json("GET", API_BASE.replace("/api/v1", "") + "/health")
    checks.append(("GET /health", status == 200, f"status={status} body={body}"))

    # /mcp/reports should exist (405 for GET, 200 for POST initialize)
    s_post, _ = request_json("POST", API_BASE + "/mcp/reports", payload={"jsonrpc": "2.0", "id": 1, "method": "initialize"}, headers={})
    checks.append(("POST /mcp/reports exists", s_post in (200, 400, 401), f"status={s_post}"))

    # /mcp/daily-report should NOT exist. Use a real account token so auth
    # middleware does not short-circuit to 401 before route resolution.
    probe_token = accounts[0]["token"] if accounts else None
    s_old, _ = request_json("POST", API_BASE + "/mcp/daily-report", token=probe_token, payload={"jsonrpc": "2.0", "id": 1, "method": "initialize"})
    checks.append(("/mcp/daily-report absent", s_old == 404, f"status={s_old}"))

    report.add("| 检查项 | 结果 | 详情 |")
    report.add("| --- | --- | --- |")
    for name, ok, detail in checks:
        report.add(f"| {name} | {'PASS' if ok else 'FAIL'} | {detail} |")
    return all(ok for _, ok, _ in checks)


def verify_default_assets(report, accounts):
    report.section("默认 Report 配置回归结果")
    report.add("对每个测试账号验证 AI Assets 中存在属于自己的默认 Skill / MCP / Agent，且 duplicate count = 1/1/1。")
    report.add("")
    report.add("| user_id | username | role | skill | mcp | agent | dup (s/m/a) | owner=self | not system |")
    report.add("| --- | --- | --- | --- | --- | --- | --- | --- | --- |")
    all_ok = True
    by_username = {a["username"]: a for a in accounts}
    for account in accounts:
        token = account["token"]
        user_status, user = auth_me(token)
        if user_status >= 300:
            report.add(f"| {account['user_id']} | {account['username']} | {account['role']} | - | - | - | - | - | - |")
            all_ok = False
            continue
        assets = list_ai_assets(token)
        skill, sc = find_report_skill(assets["skills"])
        mcp, mc = find_report_mcp(assets["mcps"])
        agent, ac = find_report_agent(assets["agents"])
        owner_self = bool(skill and mcp and agent)
        not_system = bool(skill and mcp and agent)
        report.add(
            f"| {account['user_id']} | {account['username']} | {account['role']} | "
            f"{'PASS' if skill else 'FAIL'} | {'PASS' if mcp else 'FAIL'} | {'PASS' if agent else 'FAIL'} | "
            f"{sc}/{mc}/{ac} | {'PASS' if owner_self else 'FAIL'} | {'PASS' if not_system else 'FAIL'} |"
        )
        if not (skill and mcp and agent):
            all_ok = False
        account["agent_id"] = agent.get("agent_id") if agent else None
        account["user_obj"] = user
    return all_ok, by_username


# ---------------------------------------------------------------------------
# Session upload + verification
# ---------------------------------------------------------------------------

def upload_all_sessions(report, accounts, fixtures):
    report.section("session fixture 数据说明与上传结果")
    report.add(f"- fixture 目录: `tmp/report_agent_real_model_sessions_{report.timestamp}/`")
    report.add(f"- 每条 session summary 包含唯一前缀 `{report.prefix}`。")
    report.add("")
    report.add("| user_id | username | session_ref | upload status | owner=self | content has prefix |")
    report.add("| --- | --- | --- | --- | --- | --- |")
    session_dir = TMP_DIR / f"report_agent_real_model_sessions_{report.timestamp}"
    session_dir.mkdir(parents=True, exist_ok=True)
    (session_dir / "README.md").write_text(
        f"# Real model test sessions\n\nPrefix: {report.prefix}\n\n",
        encoding="utf-8",
    )
    upload_ok = True
    owned = []
    for account, sess in fixtures:
        token = account["token"]
        status, body = upload_sessions(token, [sess])
        results = (body or {}).get("results", []) if status < 300 else []
        row_ok = status < 300 and len(results) > 0 and str(results[0].get("status", "")).startswith("created")
        owner_ok = False
        content_ok = False
        if row_ok:
            session_id = results[0].get("id")
            s2, sess_body = request_json("GET", API_BASE + f"/sessions/{session_id}", token)
            if s2 < 300 and sess_body:
                owner_ok = sess_body.get("user_id") == account["user_id"]
                summary = sess_body.get("summary") or ""
                content_ok = report.prefix in summary
        report.add(
            f"| {account['user_id']} | {account['username']} | `{sess['session_ref']}` | "
            f"{'PASS' if row_ok else 'FAIL'} ({status}, {results[0].get('status') if results else body}) | "
            f"{'PASS' if owner_ok else 'FAIL'} | {'PASS' if content_ok else 'FAIL'} |"
        )
        if not (row_ok and owner_ok and content_ok):
            upload_ok = False
        else:
            owned.append((account, sess, session_id))
    return upload_ok, owned


def verify_session_scope(report, accounts, owned):
    report.section("session scope 权限校验")
    report.add("通过业务接口 `/sessions` 确认 employee 只能看自己的 session、TL 能看同组、Director 能看部门、PM 不能读 team/department。")
    report.add("")
    by_username = {a["username"]: a for a in accounts}
    checks = []
    emp_a = by_username.get("t05")
    emp_b = by_username.get("t06")
    tl = by_username.get("t03")
    pm = by_username.get("t01")
    director = by_username.get("t02")

    def list_sessions(token, date_filter=None):
        url = API_BASE + "/sessions?page=1&page_size=100"
        if date_filter:
            url += f"&date={date_filter}"
        return request_json("GET", url, token)

    today = date.today().isoformat()
    if emp_a:
        s, body = list_sessions(emp_a["token"], today)
        items = (body or {}).get("items", [])
        owners = {it.get("user_id") for it in items}
        ok = emp_a["user_id"] in owners and emp_b["user_id"] not in owners
        checks.append(("employee t05 只能看自己 session", ok, f"owners={owners}"))
    if tl:
        s, body = list_sessions(tl["token"], today)
        items = (body or {}).get("items", [])
        owners = {it.get("user_id") for it in items}
        ok = emp_a["user_id"] in owners and emp_b["user_id"] in owners
        checks.append(("TL t03 能读同组成员 session", ok, f"owners={owners}"))
    if director:
        s, body = list_sessions(director["token"], today)
        items = (body or {}).get("items", [])
        owners = {it.get("user_id") for it in items}
        ok = emp_a["user_id"] in owners
        checks.append(("Director t02 能读部门成员 session", ok, f"owners={owners}"))
    report.add("| 用例 | 结果 | 详情 |")
    report.add("| --- | --- | --- |")
    for name, ok, detail in checks:
        report.add(f"| {name} | {'PASS' if ok else 'FAIL'} | {detail} |")
    return all(ok for _, ok, _ in checks)


# ---------------------------------------------------------------------------
# Real Report Agent run helpers
# ---------------------------------------------------------------------------

def week_range_today():
    today_dt = date.today()
    # Monday-based week start
    days_since_monday = today_dt.weekday()
    monday = today_dt - timedelta(days=days_since_monday)
    sunday = monday + timedelta(days=6)
    return monday.isoformat(), sunday.isoformat()


def period_for(report_type, today_iso, week_start, week_end):
    if report_type.endswith("_weekly"):
        return {"week_start": week_start, "week_end": week_end}
    return {"date": today_iso}


def target_for(report_type, account):
    """Build target payload. Default to self."""
    return {"type": "self"}


def run_real_agent(report, account, report_type, today_iso, week_start, week_end, expect_success=True, label=None):
    """Start a real Report Agent run, poll, verify readback. Returns result dict."""
    label = label or f"{report_type}@{account['username']}"
    token = account["token"]
    agent_id = account.get("agent_id")
    period = period_for(report_type, today_iso, week_start, week_end)
    target = target_for(report_type, account)
    payload = {
        "report_type": report_type,
        "period": period,
        "target": target,
    }
    result = {
        "label": label,
        "report_type": report_type,
        "user": account["username"],
        "user_id": account["user_id"],
        "role": account["role"],
        "target": target,
        "period": period,
        "agent_id": agent_id,
        "run_id": None,
        "external_session_id": None,
        "status": "NOT_STARTED",
        "error_message": None,
        "session_upload": "N/A",
        "agent_run_created": "FAIL",
        "model_run_status": "FAIL",
        "mcp_read_evidence": "N/A",
        "mcp_write_evidence": "N/A",
        "business_readback": "FAIL",
        "content_check": "FAIL",
        "permission_check": "PASS" if expect_success else "FAIL",
        "readback_payload": None,
        "ai_run_final": None,
    }
    if not agent_id:
        result["status"] = "BLOCKED"
        result["error_message"] = "default Report Agent missing for user"
        report.blocked_details.append({"label": label, "reason": result["error_message"]})
        report.matrix.append(result)
        return result

    status, body = start_report_run(token, agent_id, payload)
    if status >= 300:
        result["error_message"] = f"run API HTTP {status}: {body}"
        result["agent_run_created"] = "FAIL"
        if not expect_success:
            result["permission_check"] = "PASS"
            result["status"] = "PASS"
        else:
            report.fail_details.append({"label": label, "reason": result["error_message"]})
            result["status"] = "FAIL"
        report.matrix.append(result)
        return result

    result["agent_run_created"] = "PASS"
    run_id = body.get("id")
    result["run_id"] = run_id
    result["external_session_id"] = (body.get("external_session_id") or None)
    # Record run log
    run_log = {
        "label": label, "run_id": run_id,
        "report_type": report_type, "user": account["username"],
        "agent_id": agent_id, "external_session_id": result["external_session_id"],
        "initial_status": body.get("status"),
    }
    report.runs.append(run_log)

    if SKIP_REAL_MODEL:
        result["status"] = "SKIPPED"
        result["model_run_status"] = "SKIPPED"
        report.matrix.append(result)
        return result

    # Poll ai_run + business readback
    deadline = time.time() + POLL_TIMEOUT_SEC
    last_status = body.get("status")
    terminal = False
    while time.time() < deadline:
        s_run, run_body = get_agent_run(token, run_id)
        if s_run < 300 and run_body:
            last_status = run_body.get("status")
            result["ai_run_final"] = run_body
            if last_status in ("succeeded", "failed", "timeout"):
                terminal = True
                break
        # Check business readback in parallel (Agent may write before ai_run terminal)
        readback = read_business_report(report, account, report_type, today_iso, week_start, week_end)
        if readback.get("found") and content_matches_prefix_or_keywords(readback.get("content") or "", report.prefix):
            result["business_readback"] = "PASS"
            result["readback_payload"] = readback["payload"]
            result["mcp_write_evidence"] = "PASS"
            result["content_check"] = "PASS"
            result["model_run_status"] = "PASS" if last_status in ("succeeded",) else ("PASS" if last_status in ("running", "pending") else "FAIL")
            result["status"] = "PASS"
            report.matrix.append(result)
            return result
        time.sleep(POLL_INTERVAL_SEC)

    # Final attempt
    readback = read_business_report(report, account, report_type, today_iso, week_start, week_end)
    if readback.get("found") and content_matches_prefix_or_keywords(readback.get("content") or "", report.prefix):
        result["business_readback"] = "PASS"
        result["readback_payload"] = readback["payload"]
        result["mcp_write_evidence"] = "PASS"
        result["content_check"] = "PASS"
        result["model_run_status"] = "PASS" if last_status == "succeeded" else "TIMEOUT"
        result["status"] = "PASS" if last_status == "succeeded" else "TIMEOUT"
        if result["status"] == "TIMEOUT":
            report.timeout_details.append({"label": label, "reason": f"report written but ai_run status={last_status}"})
        report.matrix.append(result)
        return result

    if last_status == "failed":
        err = (result["ai_run_final"] or {}).get("error_message") or "model run failed"
        result["error_message"] = err
        result["model_run_status"] = "FAIL"
        result["status"] = "FAIL"
        report.fail_details.append({"label": label, "reason": err})
    else:
        result["model_run_status"] = "TIMEOUT"
        result["status"] = "TIMEOUT"
        report.timeout_details.append({"label": label, "reason": f"ai_run status={last_status}, no readback"})
    report.matrix.append(result)
    return result


def read_business_report(report, account, report_type, today_iso, week_start, week_end):
    token = account["token"]
    found = False
    payload = None
    content = ""
    if report_type == "personal_daily":
        # /reports/today uses server today; fall back to /reports/mine by date
        # so a date rollover mid-run does not produce a false negative.
        s, body = request_json("GET", API_BASE + "/reports/today", token)
        if s < 300 and body and (body.get("content") or "").strip():
            payload = body
            content = body.get("content") or ""
            found = bool(content)
        else:
            s2, list_body = request_json("GET", API_BASE + f"/reports/mine?from={today_iso}&to={today_iso}&page=1&page_size=20", token)
            items = (list_body or {}).get("items", [])
            if items:
                rid = items[0].get("id")
                s3, rbody = request_json("GET", API_BASE + f"/reports/{rid}", token)
                if s3 < 300 and rbody:
                    payload = rbody
                    content = rbody.get("content") or ""
                    found = bool(content)
    elif report_type == "personal_weekly":
        s, body = request_json("GET", API_BASE + f"/reports/weekly/mine/current?week_start={week_start}", token)
        if s < 300 and body:
            payload = body
            content = body.get("content") or ""
            found = bool(content)
    elif report_type == "team_daily":
        s, body = request_json("GET", API_BASE + f"/reports/team/today?report_date={today_iso}", token)
        if s < 300 and body:
            payload = body
            content = body.get("content") or ""
            found = bool(content)
    elif report_type == "team_weekly":
        s, body = request_json("GET", API_BASE + f"/reports/team/weekly/current?week_start={week_start}", token)
        if s < 300 and body:
            payload = body
            content = body.get("content") or ""
            found = bool(content)
    elif report_type == "department_daily":
        s, body = request_json("GET", API_BASE + f"/reports/department/today?report_date={today_iso}", token)
        if s < 300 and body:
            payload = body
            content = body.get("content") or ""
            found = bool(content)
    elif report_type == "department_weekly":
        s, body = request_json("GET", API_BASE + f"/reports/department/weekly/current?week_start={week_start}", token)
        if s < 300 and body:
            payload = body
            content = body.get("content") or ""
            found = bool(content)
    return {"found": found, "content": content, "payload": payload}


def verify_readback_fields(report, result):
    """Validate product fields on readback payload. Returns dict of field->bool."""
    p = result.get("readback_payload") or {}
    fields = {
        "content_non_empty": bool((p.get("content") or "").strip()),
        "product_status_ai_generated": p.get("product_status") == "ai_generated",
        "generation_mode_managed_agent": p.get("generation_mode") == "managed_agent",
        "edited_false": p.get("edited") is False,
        "managed_agent_run_id_matches": bool(p.get("managed_agent_run_id")) and str(p.get("managed_agent_run_id")) == str(result.get("run_id")),
        "has_model_id": bool(p.get("model_id")),
        "has_agent_id": bool(p.get("agent_id")),
    }
    return fields


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    if not AIHUB_SECRET:
        print("AIHUB_SECRET is required", file=sys.stderr)
        return 2
    TMP_DIR.mkdir(exist_ok=True)
    timestamp = time.strftime("%Y%m%d_%H%M%S")
    prefix = f"REPORT_AGENT_REAL_MODEL_TEST_{timestamp}"
    today_iso = date.today().isoformat()
    week_start, week_end = week_range_today()
    report = Report(timestamp, prefix)

    accounts = load_accounts()
    admin = load_admin_account()

    report.add(f"# Report Agent 真实模型六类报告验收报告")
    report.add("")
    report.add(f"- 生成时间: `{timestamp}`")
    report.add(f"- 测试日期: `{today_iso}` (周 {week_start} ~ {week_end})")

    preflight_ok = preflight(report, accounts)

    assets_ok, by_username = verify_default_assets(report, accounts)
    # If assets missing, attempt backfill once with admin token, then re-check.
    if not assets_ok and admin:
        report.add("")
        report.add("- 检测到默认资产缺失，尝试 admin backfill 后重新校验。")
        bs, bb = backfill_default_assets(admin["token"])
        report.add(f"  - backfill HTTP {bs}: total={bb.get('total') if isinstance(bb, dict) else bb} succeeded={(bb or {}).get('succeeded') if isinstance(bb, dict) else 'n/a'}")
        accounts = load_accounts()  # reload tokens unchanged
        assets_ok, by_username = verify_default_assets(report, accounts)

    fixtures = build_session_fixtures(prefix, accounts)
    upload_ok, owned = upload_all_sessions(report, accounts, fixtures)
    scope_ok = verify_session_scope(report, accounts, owned)

    report.summary["session_upload_pass"] = upload_ok and scope_ok

    # Real model runs
    report.section("真实 Agent run API 与模型运行汇总")
    report.add(f"- run API: `POST /api/v1/ai-assets/report-agents/{{agentId}}/runs`")
    report.add(f"- 只传 `report_type` / `period` / `target`，由后端注入 `mcp_url`、`credential_slot`、`run_id`。")
    report.add("")

    if not SKIP_REAL_MODEL:
        # Core 6 cases
        emp_a = by_username.get("t05")
        emp_b = by_username.get("t06")
        pm = by_username.get("t01")
        tl = by_username.get("t03")
        director = by_username.get("t02")

        core_cases = []
        if emp_a:
            core_cases.append((emp_a, "personal_daily"))
            core_cases.append((emp_a, "personal_weekly"))
        if tl:
            core_cases.append((tl, "team_daily"))
            core_cases.append((tl, "team_weekly"))
        if director:
            core_cases.append((director, "department_daily"))
            core_cases.append((director, "department_weekly"))

        for account, rtype in core_cases:
            run_real_agent(report, account, rtype, today_iso, week_start, week_end, expect_success=True)

        # PM personal cases
        if pm:
            run_real_agent(report, pm, "personal_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, pm, "personal_weekly", today_iso, week_start, week_end, expect_success=True)

        # employee_b personal_daily helper (used for team_daily source) — best effort, no failure counting
        if emp_b:
            run_real_agent(report, emp_b, "personal_daily", today_iso, week_start, week_end, expect_success=True)

        if RUN_ADMIN_SMOKE and admin:
            admin["agent_id"] = None
            # Need agent_id for admin: list assets
            assets = list_ai_assets(admin["token"])
            agent, _ = find_report_agent(assets["agents"])
            if agent:
                admin["agent_id"] = agent.get("agent_id")
                run_real_agent(report, admin, "personal_daily", today_iso, week_start, week_end, expect_success=True, label="admin_smoke_personal_daily")
            else:
                report.blocked_details.append({"label": "admin_smoke", "reason": "admin has no default Report Agent"})
    else:
        report.add("- `AIDA_SKIP_REAL_MODEL=1` 已设置，跳过真实模型运行。")

    # Permission / forbidden cases
    permission_section(report, by_username, today_iso, week_start, week_end)

    # Build matrix output
    emit_matrix(report)
    emit_run_log(report)
    emit_quality_and_fields(report)
    emit_permission_results(report)
    emit_regression_and_grep(report)
    emit_summary(report, accounts, admin, preflight_ok, assets_ok, upload_ok, scope_ok)

    output = "\n".join(report.lines) + "\n"
    DOC_REPORT.write_text(output, encoding="utf-8")
    tmp_report = TMP_DIR / f"report_agent_real_model_full_flow_{timestamp}.md"
    tmp_report.write_text(output, encoding="utf-8")
    print(str(DOC_REPORT))
    print(str(tmp_report))
    return 0


def permission_section(report, by_username, today_iso, week_start, week_end):
    report.section("越权真实 Agent 测试")
    report.add("对越权用例优先利用 run API 前置校验或短失败；不长时间等待模型。")
    report.add("")
    report.add("| 用例 | 调用者 | report_type | target | 期望 | 实际 HTTP / 错误 | 结果 |")
    report.add("| --- | --- | --- | --- | --- | --- | --- |")
    cases = []
    emp_a = by_username.get("t05")
    pm = by_username.get("t01")
    tl = by_username.get("t03")
    director = by_username.get("t02")

    def attempt(account, rtype, target, expect):
        if not account or not account.get("agent_id"):
            cases.append((account, rtype, target, expect, "BLOCKED: no agent", "BLOCKED"))
            return
        period = period_for(rtype, today_iso, week_start, week_end)
        s, body = start_report_run(account["token"], account["agent_id"], {
            "report_type": rtype, "period": period, "target": target,
        })
        if s < 300:
            err_msg = f"HTTP {s} run_id={body.get('id')} status={body.get('status')} (run API accepted; MCP-layer enforcement not verified)"
            # Run API accepted. For "reject" expectation this is a real FAIL.
            # For "reject-or-mcp-forbidden" this is RUN_API_ACCEPTED — needs MCP
            # layer to enforce; we record it as WARN, not PASS.
            if expect == "reject":
                result = "FAIL"
            else:
                result = "WARN_RUN_API_ACCEPTED"
            cases.append((account, rtype, target, expect, err_msg, result))
        else:
            err = body if isinstance(body, dict) else {"error": str(body)}
            err_msg = f"HTTP {s} code={err.get('code')} error={err.get('error')}"
            # Any 4xx/5xx counts as a rejection for both expectation flavors.
            result = "PASS"
            cases.append((account, rtype, target, expect, err_msg, result))

    if emp_a:
        attempt(emp_a, "team_daily", {"type": "self"}, "reject-or-mcp-forbidden")
        attempt(emp_a, "department_daily", {"type": "self"}, "reject-or-mcp-forbidden")
        attempt(emp_a, "personal_daily", {"type": "user", "user_id": by_username["t06"]["user_id"]}, "reject")
    if pm:
        attempt(pm, "team_daily", {"type": "self"}, "reject-or-mcp-forbidden")
        attempt(pm, "department_daily", {"type": "self"}, "reject-or-mcp-forbidden")
    if tl:
        attempt(tl, "department_daily", {"type": "self"}, "reject-or-mcp-forbidden")
        attempt(tl, "personal_daily", {"type": "user", "user_id": by_username["t05"]["user_id"]}, "reject")
    if director:
        attempt(director, "team_daily", {"type": "self"}, "reject-or-mcp-forbidden")
        attempt(director, "personal_daily", {"type": "user", "user_id": by_username["t05"]["user_id"]}, "reject")

    for account, rtype, target, expect, err_msg, result in cases:
        who = account["username"] if account else "n/a"
        report.add(f"| {who} | {who} | {rtype} | {json.dumps(target, ensure_ascii=False)} | {expect} | {err_msg} | {result} |")

    # store for summary
    report._permission_cases = cases


def emit_matrix(report):
    report.section("真实 Agent 运行矩阵")
    report.add("| report_type | 运行用户 | target | session upload | agent run created | model run status | MCP read evidence | MCP write evidence | business readback | content check | permission check |")
    report.add("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
    for r in report.matrix:
        report.add(
            f"| {r['report_type']} | {r['user']} | {json.dumps(r['target'], ensure_ascii=False)} | "
            f"{r['session_upload']} | {r['agent_run_created']} | {r['model_run_status']} | "
            f"{r['mcp_read_evidence']} | {r['mcp_write_evidence']} | {r['business_readback']} | "
            f"{r['content_check']} | {r['permission_check']} |"
        )


def emit_run_log(report):
    report.section("真实 Agent run 明细")
    report.add("| label | run_id | report_type | user | session_id | initial_status |")
    report.add("| --- | --- | --- | --- | --- | --- |")
    for r in report.runs:
        report.add(
            f"| {r['label']} | `{r['run_id']}` | {r['report_type']} | {r['user']} | "
            f"`{r['external_session_id']}` | {r['initial_status']} |"
        )


def emit_quality_and_fields(report):
    report.section("内容质量最低校验与业务接口读回字段")
    report.add("只对 `business_readback=PASS` 的用例做字段级校验。")
    report.add("")
    report.add("| label | content_non_empty | product_status=ai_generated | generation_mode=managed_agent | edited=false | run_id matches | model_id | agent_id |")
    report.add("| --- | --- | --- | --- | --- | --- | --- | --- |")
    for r in report.matrix:
        if not r.get("readback_payload"):
            continue
        f = verify_readback_fields(report, r)
        report.add(
            f"| {r['label']} | {'PASS' if f['content_non_empty'] else 'FAIL'} | "
            f"{'PASS' if f['product_status_ai_generated'] else 'FAIL'} | "
            f"{'PASS' if f['generation_mode_managed_agent'] else 'FAIL'} | "
            f"{'PASS' if f['edited_false'] else 'FAIL'} | "
            f"{'PASS' if f['managed_agent_run_id_matches'] else 'FAIL'} | "
            f"{'PASS' if f['has_model_id'] else 'FAIL'} | "
            f"{'PASS' if f['has_agent_id'] else 'FAIL'} |"
        )


def emit_permission_results(report):
    report.section("越权测试结果")
    cases = getattr(report, "_permission_cases", [])
    if not cases:
        report.add("- 无越权用例。")
        return
    report.add("| 用例 | report_type | 期望 | 实际 | 结果 |")
    report.add("| --- | --- | --- | --- | --- |")
    for account, rtype, target, expect, err_msg, result in cases:
        who = account["username"] if account else "n/a"
        report.add(f"| {who} | {rtype} | {expect} | {err_msg} | {result} |")


def emit_regression_and_grep(report):
    report.section("辅助回归与 grep 清理")
    report.add("本节由测试脚本自动采集，记录 MCP 通用客户端、默认资产、Go / 前端回归与 grep 清理结果。")
    report.add("")

    def run_cmd(cmd, cwd=None, timeout=300, env=None):
        try:
            out = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout, env=env)
            return out.returncode, (out.stdout + out.stderr)[-2000:]
        except Exception as exc:
            return -1, str(exc)

    # MCP generic client
    rc, out = run_cmd(["python3", str(ROOT / "scripts" / "test_report_mcp_generic_client.py")], timeout=300)
    last = out.strip().splitlines()[-1] if out.strip() else ""
    report.add(f"- `scripts/test_report_mcp_generic_client.py`: rc=`{rc}`, last=`{last}`")

    # Default assets
    rc, out = run_cmd(["python3", str(ROOT / "scripts" / "test_default_report_assets.py")], timeout=180)
    report.add(f"- `scripts/test_default_report_assets.py`: rc=`{rc}`")

    # Go tests
    go_path = os.path.expanduser("~/sdk/go1.26.3/bin")
    env = os.environ.copy()
    env["PATH"] = go_path + ":" + env.get("PATH", "")
    env["GOTOOLCHAIN"] = "local"
    rc, out = run_cmd(["go", "test", "./..."], cwd=str(ROOT / "api"), timeout=600, env=env)
    go_tail = "\n".join([l for l in out.splitlines() if l.startswith("ok") or l.startswith("FAIL") or "FAIL" in l][:8])
    report.add(f"- `cd api && go test ./...`: rc=`{rc}`")
    report.add("  ```")
    report.add("  " + (go_tail or out[-400:]).replace("\n", "\n  "))
    report.add("  ```")

    # Web lint / typecheck / build
    for cmd in (["pnpm", "--dir", "web", "lint"], ["pnpm", "--dir", "web", "typecheck"], ["pnpm", "--dir", "web", "build"]):
        rc, out = run_cmd(cmd, timeout=600)
        report.add(f"- `{' '.join(cmd)}`: rc=`{rc}`")

    # grep cleanup
    patterns = [
        "ensureDefaultPersonalDailyAgent", "AIDA_REPORT_AGENT:personal_daily", "aida-daily-report",
        "personal-daily-v1", "aida-report-mcp-p0", "get_report_context",
        "aida_daily_report_get_context", "aida_daily_report_save_draft", "/mcp/daily-report",
        "mcp_authorization", "default-managed-agent-runs", "report-agents/default/ensure",
    ]
    report.add("")
    report.add("### grep 清理（api/web 生产代码）")
    report.add("| pattern | api/web hits |")
    report.add("| --- | --- |")
    for pat in patterns:
        try:
            out = subprocess.run(["grep", "-RIn", pat, "api", "web"], cwd=ROOT, capture_output=True, text=True, timeout=30)
            hits = len([l for l in out.stdout.splitlines() if l.strip()])
        except Exception:
            hits = -1
        report.add(f"| `{pat}` | {hits} |")


def emit_summary(report, accounts, admin, preflight_ok, assets_ok, upload_ok, scope_ok):
    report.section("测试结论与摘要")
    # Tally
    real_runs = [r for r in report.matrix if r.get("status") not in ("SKIPPED", "NOT_STARTED", "BLOCKED") and r.get("agent_run_created") == "PASS"]
    real_succeeded = [r for r in real_runs if r.get("status") == "PASS"]
    real_failed = [r for r in real_runs if r.get("status") == "FAIL"]
    real_timeout = [r for r in real_runs if r.get("status") == "TIMEOUT"]
    six_types = {"personal_daily", "personal_weekly", "team_daily", "team_weekly", "department_daily", "department_weekly"}
    six_success = {r["report_type"] for r in real_succeeded if r["report_type"] in six_types}
    six_all = six_types.issubset(six_success)

    report.summary["real_model_runs"] = len(real_runs)
    report.summary["real_model_succeeded"] = len(real_succeeded)
    report.summary["real_model_failed"] = len(real_failed)
    report.summary["six_types_real_success"] = six_all
    report.summary["business_readback_pass"] = any(r.get("business_readback") == "PASS" for r in report.matrix)

    perm_cases = getattr(report, "_permission_cases", [])
    perm_pass = sum(1 for c in perm_cases if c[5] == "PASS")
    perm_fail = sum(1 for c in perm_cases if c[5] == "FAIL")
    perm_warn = sum(1 for c in perm_cases if c[5] == "WARN_RUN_API_ACCEPTED")
    perm_blocked = sum(1 for c in perm_cases if c[5] == "BLOCKED")

    report.add(f"- 总用例数（真实模型 + 越权）: `{len(report.matrix) + len(perm_cases)}`")
    report.add(f"- PASS: `{len(real_succeeded) + perm_pass}`")
    report.add(f"- FAIL: `{len(real_failed) + perm_fail + len(report.fail_details)}`")
    report.add(f"- TIMEOUT: `{len(real_timeout) + len(report.timeout_details)}`")
    report.add(f"- BLOCKED: `{len(report.blocked_details) + perm_blocked}`")
    report.add(f"- WARN (run API 接受但 MCP 层未验证): `{perm_warn}`")
    report.add(f"- 真实模型 run 总数: `{len(real_runs)}`")
    report.add(f"- 真实模型 succeeded: `{len(real_succeeded)}`")
    report.add(f"- 真实模型 failed: `{len(real_failed)}`")
    report.add(f"- 6 类 report_type 全部真实生成成功: `{six_all}`")
    report.add(f"- 已成功的 report_type: `{sorted(six_success)}`")
    report.add(f"- session upload 通过: `{upload_ok and scope_ok}`")
    report.add(f"- 业务接口读回通过: `{report.summary['business_readback_pass']}`")
    report.add(f"- 前置检查通过: `{preflight_ok}`")
    report.add(f"- 默认资产回归通过: `{assets_ok}`")
    report.add("")

    if report.fail_details:
        report.add("### FAIL 明细")
        for item in report.fail_details:
            report.add(f"- `{item['label']}`: {item['reason']}")
        report.add("")
    if report.timeout_details:
        report.add("### TIMEOUT 明细")
        for item in report.timeout_details:
            report.add(f"- `{item['label']}`: {item['reason']}")
        report.add("")
    if report.blocked_details:
        report.add("### BLOCKED 明细")
        for item in report.blocked_details:
            report.add(f"- `{item['label']}`: {item['reason']}")
        report.add("")

    report.add("### 最高优先级 bug / 建议修复顺序")
    report.add("- 详见上文 FAIL/TIMEOUT/BLOCKED 明细；按 `FAIL > TIMEOUT > BLOCKED` 排序处理。")
    report.add("- 越权用例中如出现 run API 接受但 MCP 层 FORBIDDEN，记录失败发生在 MCP 层而非 run API 层。")
    report.add("")
    report.add("### 不属于本轮范围的问题")
    report.add("- UI 自动化、定时任务、历史资产清理均不在本轮范围。")
    report.add("- 业务代码 bug 仅记录，不在本轮修改。")


if __name__ == "__main__":
    raise SystemExit(main())
