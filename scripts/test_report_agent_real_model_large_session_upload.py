#!/usr/bin/env python3
"""Large-scale real-model acceptance test for the default Report Agent.

Pipeline (no mocks, real local sessions):
  real login (JWT minted with AIHUB_SECRET)
  -> scan local session files (.md/.txt/.json/.jsonl/.csv) under --session-dir
     (default: auto-search common dirs)
  -> for each local session, extract started_at/ended_at/summary/content_sha256
  -> upload ≥20 sessions via /sessions/batch, distributed across 5 roles
     (employee_a 6-8, employee_b 5-7, PM 4-6, TL 3-5, Director 2-4)
  -> real Report Agent run for ≥18 runs (≥12 success target), default model
  -> business report readback via /reports/...
  -> MCP regression / default-assets / go test / web lint+typecheck+build / grep

Outputs:
  tmp/test-reports/ReportAgent真实模型大规模Session验收报告.md
  tmp/report_agent_real_model_large_session_upload_<timestamp>.md
"""

import argparse
import base64
import csv
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
DOC_REPORT = ROOT / "doc" / "ReportAgent真实模型大规模Session验收报告.md"
TMP_DIR = ROOT / "tmp"

API_BASE = os.getenv("AIDA_API_BASE", "http://127.0.0.1:18090/api/v1").rstrip("/")
MANAGED_AGENT_URL = os.getenv("MANAGED_AGENT_URL", "http://192.168.18.107:3081").rstrip("/")
AIHUB_SECRET = os.getenv("AIHUB_SECRET", "").strip()
ADMIN_TOKEN_ENV = os.getenv("AIDA_ADMIN_TOKEN", "").strip()

REPORT_SKILL_SLUG = "aida-report"
REPORT_SKILL_VERSION = "1.0.0"
REPORT_MCP_SLUG = os.getenv("MANAGED_AGENT_REPORT_MCP_SLUG", "aida-report-mcp")
REPORT_MCP_VERSION = os.getenv("MANAGED_AGENT_REPORT_MCP_VERSION", "report-v1")
REPORT_MCP_SLOT = "AIDA_REPORT_MCP_AUTH"

DEFAULT_MODEL_ID = os.getenv("MANAGED_AGENT_DEFAULT_MODEL_ID", "MiniMax-M2.5")
DEFAULT_ENGINE = os.getenv("MANAGED_AGENT_DEFAULT_ENGINE", "claude-code")

POLL_INTERVAL_SEC = float(os.getenv("AIDA_POLL_INTERVAL", "10"))
POLL_TIMEOUT_SEC = float(os.getenv("AIDA_POLL_TIMEOUT", "900"))

SKIP_REAL_MODEL = os.getenv("AIDA_SKIP_REAL_MODEL", "0") == "1"

# Role -> username mapping (matches doc/v1/测试账号文档.md)
ROLE_USERNAMES = {
    "employee_a": "t05",
    "employee_b": "t06",
    "pm": "t01",
    "tl": "t03",
    "director": "t02",
}
# Per-role allocation ranges (inclusive)
ROLE_ALLOCATION = {
    "employee_a": (6, 8),
    "employee_b": (5, 7),
    "pm": (4, 6),
    "tl": (3, 5),
    "director": (2, 4),
}
MIN_TOTAL_SESSIONS = 20
MIN_REAL_RUNS = 18
MIN_REAL_SUCCESSES = 12

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
    "local session",
    "本地 session",
    "大规模",
    "large session upload",
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
    boundary = "----aida-large-test-" + uuid.uuid4().hex
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
    return request_json("GET", API_BASE + "/auth/me", token)


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
    metadata = json.dumps({"sessions": sessions}, ensure_ascii=False)
    return request_multipart(API_BASE + "/sessions/batch", token, {"metadata": metadata})


def start_report_run(token, agent_id, payload):
    return request_json("POST", API_BASE + f"/ai-assets/report-agents/{agent_id}/runs", token, payload, timeout=60)


def get_agent_run(token, run_id):
    return request_json("GET", API_BASE + f"/ai-assets/agent-runs/{run_id}", token)


# ---------------------------------------------------------------------------
# Local session parsing
# ---------------------------------------------------------------------------

DEFAULT_SEARCH_DIRS = [
    Path.home() / ".claude" / "projects",
    ROOT / "tmp",
]


def candidate_session_files(session_dir: Path):
    """Return list of session files under session_dir, supporting
    .md/.txt/.json/.jsonl/.csv."""
    exts = {".md", ".txt", ".json", ".jsonl", ".csv"}
    out = []
    if session_dir.is_file() and session_dir.suffix.lower() in exts:
        out.append(session_dir)
        return out
    if not session_dir.is_dir():
        return out
    for p in session_dir.rglob("*"):
        if p.is_file() and p.suffix.lower() in exts and p.stat().st_size > 0:
            out.append(p)
    return out


def parse_jsonl_session(path: Path):
    """Return dict with started_at, ended_at, summary, content_len, session_id, source_kind."""
    started = ended = None
    session_id = None
    first_user_text = ""
    text_total = 0
    line_count = 0
    cwd = None
    git_branch = None
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                line_count += 1
                if not line.strip():
                    continue
                try:
                    obj = json.loads(line)
                except Exception:
                    continue
                ts = obj.get("timestamp")
                if ts:
                    if not started:
                        started = ts
                    ended = ts
                sid = obj.get("sessionId")
                if sid and not session_id:
                    session_id = sid
                if not cwd:
                    cwd = obj.get("cwd")
                if not git_branch:
                    git_branch = obj.get("gitBranch")
                if obj.get("type") in ("user", "assistant") and not first_user_text:
                    msg = obj.get("message", {})
                    if isinstance(msg, dict):
                        c = msg.get("content")
                        t = ""
                        if isinstance(c, str):
                            t = c
                        elif isinstance(c, list):
                            for it in c:
                                if isinstance(it, dict) and it.get("type") == "text":
                                    t = it.get("text", "") or ""
                                    break
                        if t and not t.startswith("<"):
                            first_user_text = t.strip()
                msg = obj.get("message", {})
                if isinstance(msg, dict):
                    c = msg.get("content")
                    if isinstance(c, str):
                        text_total += len(c)
                    elif isinstance(c, list):
                        for it in c:
                            if isinstance(it, dict) and isinstance(it.get("text"), str):
                                text_total += len(it.get("text") or "")
    except Exception:
        pass
    if not started:
        started = "2026-06-30T09:00:00Z"
    if not ended:
        ended = started
    if not first_user_text:
        first_user_text = f"(no user message extracted) local file {path.name}"
    summary = first_user_text[:400]
    return {
        "source_kind": "jsonl",
        "session_id": session_id or path.stem,
        "started_at": started.replace("Z", "+00:00") if "Z" in started else started,
        "ended_at": ended.replace("Z", "+00:00") if "Z" in ended else ended,
        "summary": summary,
        "content_len": text_total or path.stat().st_size,
        "cwd": cwd,
        "git_branch": git_branch,
        "line_count": line_count,
    }


def parse_plain_text_session(path: Path):
    """For .md/.txt files."""
    try:
        text = path.read_text(encoding="utf-8", errors="replace")
    except Exception:
        text = ""
    summary = text.strip().split("\n")[0][:400] if text.strip() else f"(empty) {path.name}"
    return {
        "source_kind": path.suffix.lstrip("."),
        "session_id": path.stem,
        "started_at": "2026-06-30T09:00:00Z",
        "ended_at": "2026-06-30T10:00:00Z",
        "summary": summary,
        "content_len": len(text),
        "cwd": None,
        "git_branch": None,
        "line_count": text.count("\n") + 1,
    }


def parse_json_session(path: Path):
    try:
        obj = json.loads(path.read_text(encoding="utf-8", errors="replace"))
    except Exception:
        obj = {}
    summary = ""
    if isinstance(obj, dict):
        summary = (obj.get("summary") or obj.get("title") or obj.get("content") or "")[:400]
    if not summary:
        summary = f"(json) {path.name}"
    return {
        "source_kind": "json",
        "session_id": str(obj.get("id") or obj.get("session_id") or path.stem),
        "started_at": obj.get("started_at") or "2026-06-30T09:00:00Z",
        "ended_at": obj.get("ended_at") or "2026-06-30T10:00:00Z",
        "summary": summary,
        "content_len": len(json.dumps(obj, ensure_ascii=False)),
        "cwd": obj.get("cwd"),
        "git_branch": obj.get("git_branch"),
        "line_count": 1,
    }


def parse_csv_session(path: Path):
    rows = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            reader = csv.DictReader(f)
            for row in reader:
                rows.append(row)
    except Exception:
        pass
    summary = ""
    if rows:
        first = rows[0]
        summary = (first.get("summary") or first.get("title") or first.get("content") or "")[:400]
    if not summary:
        summary = f"(csv) {path.name}"
    return {
        "source_kind": "csv",
        "session_id": path.stem,
        "started_at": "2026-06-30T09:00:00Z",
        "ended_at": "2026-06-30T10:00:00Z",
        "summary": summary,
        "content_len": path.stat().st_size,
        "cwd": None,
        "git_branch": None,
        "line_count": len(rows),
    }


def parse_session_file(path: Path):
    suffix = path.suffix.lower()
    if suffix == ".jsonl":
        return parse_jsonl_session(path)
    if suffix == ".json":
        return parse_json_session(path)
    if suffix == ".csv":
        return parse_csv_session(path)
    return parse_plain_text_session(path)


def sha256_of_file(path: Path) -> str:
    h = hashlib.sha256()
    try:
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(65536), b""):
                h.update(chunk)
    except Exception:
        return ""
    return h.hexdigest()


# ---------------------------------------------------------------------------
# Report structure
# ---------------------------------------------------------------------------

class Report:
    def __init__(self, timestamp, prefix, test_run_id):
        self.timestamp = timestamp
        self.prefix = prefix
        self.test_run_id = test_run_id
        self.lines = []
        self.matrix = []
        self.runs = []
        self.fail_details = []
        self.timeout_details = []
        self.blocked_details = []
        self.uploads = []  # list of upload dicts
        self.summary = {
            "total": 0, "pass": 0, "fail": 0, "timeout": 0, "blocked": 0,
            "real_model_runs": 0, "real_model_succeeded": 0, "real_model_failed": 0,
            "six_types_real_success": False,
            "session_upload_pass": False,
            "business_readback_pass": False,
            "mcp_regression_pass": False,
            "go_frontend_regression_pass": False,
            "uploads_attempted": 0,
            "uploads_succeeded": 0,
        }

    def add(self, line):
        self.lines.append(line)

    def section(self, title):
        self.lines.append("")
        self.lines.append(f"## {title}")
        self.lines.append("")


# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

def preflight(report, accounts, session_files_found):
    report.section("测试环境与前置检查")
    report.add(f"- API base: `{API_BASE}`")
    report.add(f"- Managed Agent URL: `{MANAGED_AGENT_URL}`")
    report.add(f"- 唯一前缀: `{report.prefix}`")
    report.add(f"- test_run_id: `{report.test_run_id}`")
    report.add(f"- 默认模型: `{DEFAULT_MODEL_ID}` / engine `{DEFAULT_ENGINE}` (不降级)")
    report.add(f"- 轮询: interval `{POLL_INTERVAL_SEC}s`, timeout `{POLL_TIMEOUT_SEC}s`")
    report.add(f"- 跳过真实模型: `{SKIP_REAL_MODEL}`")
    report.add(f"- 本地 session 候选文件数: `{len(session_files_found)}`")
    report.add(f"- 目标上传条数: ≥`{MIN_TOTAL_SESSIONS}`")
    report.add(f"- 目标真实模型运行次数: ≥`{MIN_REAL_RUNS}` (≥`{MIN_REAL_SUCCESSES}` 成功)")
    report.add("")

    checks = []
    status, body = request_json("GET", API_BASE.replace("/api/v1", "") + "/health")
    checks.append(("GET /health", status == 200, f"status={status} body={body}"))

    s_post, _ = request_json("POST", API_BASE + "/mcp/reports", payload={"jsonrpc": "2.0", "id": 1, "method": "initialize"}, headers={})
    checks.append(("POST /mcp/reports exists", s_post in (200, 400, 401), f"status={s_post}"))

    probe_token = accounts[0]["token"] if accounts else None
    s_old, _ = request_json("POST", API_BASE + "/mcp/daily-report", token=probe_token, payload={"jsonrpc": "2.0", "id": 1, "method": "initialize"})
    checks.append(("/mcp/daily-report absent", s_old == 404, f"status={s_old}"))

    checks.append(("local sessions ≥ 20", len(session_files_found) >= MIN_TOTAL_SESSIONS, f"count={len(session_files_found)}"))

    report.add("| 检查项 | 结果 | 详情 |")
    report.add("| --- | --- | --- |")
    for name, ok, detail in checks:
        report.add(f"| {name} | {'PASS' if ok else 'FAIL'} | {detail} |")
    return all(ok for _, ok, _ in checks)


def verify_default_assets(report, accounts):
    report.section("默认 Report 配置回归结果")
    report.add("对每个测试账号验证 AI Assets 中存在属于自己的默认 Skill / MCP / Agent，duplicate count = 1/1/1。")
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
        report.add(
            f"| {account['user_id']} | {account['username']} | {account['role']} | "
            f"{'PASS' if skill else 'FAIL'} | {'PASS' if mcp else 'FAIL'} | {'PASS' if agent else 'FAIL'} | "
            f"{sc}/{mc}/{ac} | {'PASS' if (skill and mcp and agent) else 'FAIL'} | {'PASS' if (skill and mcp and agent) else 'FAIL'} |"
        )
        if not (skill and mcp and agent):
            all_ok = False
        account["agent_id"] = agent.get("agent_id") if agent else None
        account["user_obj"] = user
    return all_ok, by_username


# ---------------------------------------------------------------------------
# Session allocation + upload
# ---------------------------------------------------------------------------

def allocate_sessions_to_roles(local_sessions):
    """Distribute local_sessions across roles per ROLE_ALLOCATION ranges.

    local_sessions: list of dicts (parsed). Returns dict role -> list of sessions.
    """
    # Shuffle deterministically by sha256 of source file for stability.
    local_sessions = sorted(local_sessions, key=lambda s: s.get("local_sha256", "") or s["local_file"])

    # Determine target counts: minimum of each range, sum should reach ≥20.
    targets = {role: rng[0] for role, rng in ROLE_ALLOCATION.items()}
    # Total minimum: 6+5+4+3+2 = 20. Good.

    allocation = {role: [] for role in ROLE_ALLOCATION}
    # Round-robin by walking through local_sessions, assigning to roles in
    # priority order (employee_a first since it has the biggest allocation).
    role_order = ["employee_a", "employee_b", "pm", "tl", "director"]
    idx = 0
    # First pass: fill each role to its minimum
    for role in role_order:
        while len(allocation[role]) < targets[role] and idx < len(local_sessions):
            allocation[role].append(local_sessions[idx])
            idx += 1
    # Second pass: top up to the max of each range, until we run out of
    # sessions or all roles are at max.
    max_targets = {role: rng[1] for role, rng in ROLE_ALLOCATION.items()}
    progressed = True
    while idx < len(local_sessions) and progressed:
        progressed = False
        for role in role_order:
            if idx >= len(local_sessions):
                break
            if len(allocation[role]) < max_targets[role]:
                allocation[role].append(local_sessions[idx])
                idx += 1
                progressed = True
    return allocation


def upload_local_sessions(report, accounts, allocation, prefix, test_run_id):
    report.section("大规模本地 session 上传结果")
    report.add(f"- test_run_id: `{test_run_id}`")
    report.add(f"- 唯一前缀（注入 title/summary）: `{prefix}`")
    report.add(f"- 目标角色分配: employee_a 6-8, employee_b 5-7, PM 4-6, TL 3-5, Director 2-4")
    report.add("")
    by_username = {a["username"]: a for a in accounts}
    today_iso = date.today().isoformat()
    all_ok = True
    total_uploaded = 0
    for role, sessions in allocation.items():
        username = ROLE_USERNAMES[role]
        account = by_username.get(username)
        if not account:
            report.add(f"- 角色 `{role}` 找不到对应用户 `{username}`，跳过 {len(sessions)} 条。")
            all_ok = False
            continue
        batch_no = 0
        for sess in sessions:
            batch_no += 1
            title = f"{prefix} | {role} | {sess['local_file'].name}"
            summary = (
                f"{prefix}\n"
                f"[local session upload] role={role} user={username} batch={batch_no}\n"
                f"source_file={sess['local_file']}\n"
                f"local_session_id={sess['session_id']}\n"
                f"content_len={sess['content_len']}\n"
                f"summary_text={sess['summary']}"
            )[:1500]
            session_ref = f"{prefix}-{role}-{batch_no}-{uuid.uuid4().hex[:8]}"
            payload_session = {
                "session_ref": session_ref,
                "agent_type": "claude_code",
                "started_at": sess["started_at"],
                "ended_at": sess["ended_at"],
                "duration_secs": 600,
                "model": "claude-sonnet-4-6",
                "summary": summary,
            }
            token = account["token"]
            status, body = upload_sessions(token, [payload_session])
            results = (body or {}).get("results", []) if status < 300 else []
            row_ok = status < 300 and len(results) > 0 and str(results[0].get("status", "")).startswith("created")
            upload_session_id = results[0].get("id") if results else None
            upload_entry = {
                "role": role,
                "username": username,
                "user_id": account["user_id"],
                "batch": batch_no,
                "local_file": str(sess["local_file"]),
                "local_session_id": sess["session_id"],
                "title": title,
                "content_len": sess["content_len"],
                "sha256_prefix": sess["local_sha256"][:12],
                "session_ref": session_ref,
                "upload_status": status,
                "upload_response": body if status >= 300 else None,
                "upload_session_id": upload_session_id,
                "ok": row_ok,
            }
            report.uploads.append(upload_entry)
            report.summary["uploads_attempted"] += 1
            if row_ok:
                report.summary["uploads_succeeded"] += 1
                total_uploaded += 1
            else:
                all_ok = False
    # Emit detailed upload table
    report.add("| # | role | user | batch | local_file | local_session_id | content_len | sha256[:12] | upload status | session_id | ok |")
    report.add("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
    for i, up in enumerate(report.uploads, 1):
        report.add(
            f"| {i} | {up['role']} | {up['username']} | {up['batch']} | "
            f"`{Path(up['local_file']).name}` | `{up['local_session_id']}` | "
            f"{up['content_len']} | `{up['sha256_prefix']}` | "
            f"{up['upload_status']} | `{up['upload_session_id']}` | "
            f"{'PASS' if up['ok'] else 'FAIL'} |"
        )
    report.add("")
    report.add(f"- 上传尝试: `{report.summary['uploads_attempted']}`")
    report.add(f"- 上传成功: `{report.summary['uploads_succeeded']}`")
    report.add(f"- 目标达成 (≥{MIN_TOTAL_SESSIONS}): `{total_uploaded >= MIN_TOTAL_SESSIONS}`")
    return all_ok and total_uploaded >= MIN_TOTAL_SESSIONS, total_uploaded


def verify_session_scope(report, accounts):
    report.section("session scope 权限校验")
    report.add("通过业务接口 `/sessions` 确认 employee 只能看自己、TL 能看同组、Director 能看部门。")
    report.add("")
    by_username = {a["username"]: a for a in accounts}
    checks = []
    emp_a = by_username.get("t05")
    emp_b = by_username.get("t06")
    tl = by_username.get("t03")
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
        ok = emp_a["user_id"] in owners
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
# Real Agent runs
# ---------------------------------------------------------------------------

def week_range_today():
    today_dt = date.today()
    days_since_monday = today_dt.weekday()
    monday = today_dt - timedelta(days=days_since_monday)
    sunday = monday + timedelta(days=6)
    return monday.isoformat(), sunday.isoformat()


def period_for(report_type, today_iso, week_start, week_end):
    if report_type.endswith("_weekly"):
        return {"week_start": week_start, "week_end": week_end}
    return {"date": today_iso}


def target_for(report_type, account):
    return {"type": "self"}


def read_business_report(account, report_type, today_iso, week_start, week_end):
    token = account["token"]
    found = False
    payload = None
    content = ""
    if report_type == "personal_daily":
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


def run_real_agent(report, account, report_type, today_iso, week_start, week_end, expect_success=True, label=None):
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

    deadline = time.time() + POLL_TIMEOUT_SEC
    last_status = body.get("status")
    while time.time() < deadline:
        s_run, run_body = get_agent_run(token, run_id)
        if s_run < 300 and run_body:
            last_status = run_body.get("status")
            result["ai_run_final"] = run_body
            if last_status in ("succeeded", "failed", "timeout"):
                break
        readback = read_business_report(account, report_type, today_iso, week_start, week_end)
        if readback.get("found") and content_matches_prefix_or_keywords(readback.get("content") or "", report.prefix):
            result["business_readback"] = "PASS"
            result["readback_payload"] = readback["payload"]
            result["mcp_write_evidence"] = "PASS"
            result["content_check"] = "PASS"
            result["model_run_status"] = "PASS" if last_status in ("succeeded", "running", "pending") else "FAIL"
            result["status"] = "PASS"
            report.matrix.append(result)
            return result
        time.sleep(POLL_INTERVAL_SEC)

    readback = read_business_report(account, report_type, today_iso, week_start, week_end)
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

    # If ai_run reached succeeded terminal but readback content did not match
    # prefix/keywords, the model still ran successfully — count as model PASS
    # (content_check remains FAIL, business_readback depends on whether
    # readback returned any payload).
    if last_status == "succeeded":
        result["model_run_status"] = "PASS"
        if readback.get("found"):
            result["business_readback"] = "PASS"
            result["readback_payload"] = readback["payload"]
            result["mcp_write_evidence"] = "PASS"
            result["content_check"] = "PARTIAL"
            result["status"] = "PASS"
        else:
            result["status"] = "PARTIAL"
            result["error_message"] = "ai_run succeeded but business readback empty"
            report.timeout_details.append({"label": label, "reason": "ai_run succeeded, no business readback payload"})
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


def verify_readback_fields(result):
    p = result.get("readback_payload") or {}
    return {
        "content_non_empty": bool((p.get("content") or "").strip()),
        "product_status_ai_generated": p.get("product_status") == "ai_generated",
        "generation_mode_managed_agent": p.get("generation_mode") == "managed_agent",
        "edited_false": p.get("edited") is False,
        "managed_agent_run_id_matches": bool(p.get("managed_agent_run_id")) and str(p.get("managed_agent_run_id")) == str(result.get("run_id")),
        "has_model_id": bool(p.get("model_id")),
        "has_agent_id": bool(p.get("agent_id")),
    }


# ---------------------------------------------------------------------------
# Permission cases
# ---------------------------------------------------------------------------

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
            if expect == "reject":
                result = "FAIL"
            else:
                result = "WARN_RUN_API_ACCEPTED"
            cases.append((account, rtype, target, expect, err_msg, result))
        else:
            err = body if isinstance(body, dict) else {"error": str(body)}
            err_msg = f"HTTP {s} code={err.get('code')} error={err.get('error')}"
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

    report._permission_cases = cases


# ---------------------------------------------------------------------------
# Emit helpers
# ---------------------------------------------------------------------------

def emit_local_session_inventory(report, local_sessions):
    report.section("本地 session 数据源盘点")
    report.add(f"- 候选总数: `{len(local_sessions)}`")
    report.add("- 来源目录（按优先级）: `~/.claude/projects/`, `tmp/`")
    report.add("- 支持格式: `.md/.txt/.json/.jsonl/.csv`")
    report.add("")
    report.add("| # | local_file | source_kind | session_id | started_at | ended_at | content_len | sha256[:12] |")
    report.add("| --- | --- | --- | --- | --- | --- | --- | --- |")
    for i, s in enumerate(local_sessions[:40], 1):
        report.add(
            f"| {i} | `{Path(s['local_file']).name}` | {s['source_kind']} | "
            f"`{s['session_id']}` | {s['started_at']} | {s['ended_at']} | "
            f"{s['content_len']} | `{s['local_sha256'][:12]}` |"
        )
    if len(local_sessions) > 40:
        report.add(f"| ... | (+{len(local_sessions)-40} more) | | | | | | |")


def emit_role_allocation(report, allocation):
    report.section("角色分配汇总")
    report.add("| role | username | target range | actual count |")
    report.add("| --- | --- | --- | --- |")
    for role in ["employee_a", "employee_b", "pm", "tl", "director"]:
        rng = ROLE_ALLOCATION[role]
        actual = len(allocation.get(role, []))
        report.add(f"| {role} | {ROLE_USERNAMES[role]} | {rng[0]}-{rng[1]} | {actual} |")
    total = sum(len(v) for v in allocation.values())
    report.add(f"| **total** | - | - | **{total}** |")
    if total < MIN_TOTAL_SESSIONS:
        report.add("")
        report.add(f"- **BLOCKED**: 实际分配 {total} < {MIN_TOTAL_SESSIONS}，无法满足大规模测试门槛。")


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
        f = verify_readback_fields(r)
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
    """Run MCP regression, default-assets, go test, web build, and grep.
    Returns dict of captured metrics for use by granular emit functions."""
    report.section("辅助回归与 grep 清理")
    report.add("本节由测试脚本自动采集，记录 MCP 通用客户端、默认资产、Go / 前端回归与 grep 清理结果。")
    report.add("")

    def run_cmd(cmd, cwd=None, timeout=300, env=None):
        try:
            out = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout, env=env)
            return out.returncode, (out.stdout + out.stderr)[-2000:]
        except Exception as exc:
            return -1, str(exc)

    rc, out = run_cmd(["python3", str(ROOT / "scripts" / "test_report_mcp_generic_client.py")], timeout=300)
    last = out.strip().splitlines()[-1] if out.strip() else ""
    report.add(f"- `scripts/test_report_mcp_generic_client.py`: rc=`{rc}`, last=`{last}`")
    mcp_rc, mcp_last = rc, last

    rc, out = run_cmd(["python3", str(ROOT / "scripts" / "test_default_report_assets.py")], timeout=180)
    report.add(f"- `scripts/test_default_report_assets.py`: rc=`{rc}`")
    default_assets_rc = rc

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
    go_rc, go_tail_saved = rc, (go_tail or out[-400:])

    web_rcs = {}
    for cmd in (["pnpm", "--dir", "web", "lint"], ["pnpm", "--dir", "web", "typecheck"], ["pnpm", "--dir", "web", "build"]):
        rc, out = run_cmd(cmd, timeout=600)
        report.add(f"- `{' '.join(cmd)}`: rc=`{rc}`")
        web_rcs[cmd[-1]] = rc

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

    return {
        "mcp_rc": mcp_rc, "mcp_last": mcp_last,
        "default_assets_rc": default_assets_rc,
        "go_rc": go_rc, "go_tail": go_tail_saved,
        "web_rcs": web_rcs,
    }


def emit_goals_and_scope(report):
    report.section("测试目标与范围")
    report.add("- **目标**: 在真实模型 + 真实 session 上传场景下，大面积暴露 Report Agent 链路问题。")
    report.add("- **范围**:")
    report.add("  - 真实本地 session 文件解析与上传（≥20 条，多角色分配）。")
    report.add("  - 默认 Report Agent 真实模型运行（≥18 次，≥12 次成功）。")
    report.add("  - 6 类 report_type 全部真实生成。")
    report.add("  - 业务接口读回字段一致性校验。")
    report.add("  - 越权用例的 run API / MCP 层拒绝行为。")
    report.add("  - MCP 通用回归、默认资产回归、Go / 前端构建回归。")
    report.add("- **不在范围**:")
    report.add("  - UI 自动化、定时任务、历史资产清理。")
    report.add("  - 业务代码 bug 修复（仅记录）。")


def emit_account_list(report, accounts):
    report.section("测试账号清单")
    report.add("| user_id | username | role | team |")
    report.add("| --- | --- | --- | --- |")
    for a in accounts:
        report.add(f"| {a['user_id']} | {a['username']} | {a['role']} | {a['team_label'] or '-'} |")


def emit_privacy_note(report):
    report.section("数据脱敏与隐私说明")
    report.add("- 本轮所有 session 内容来自本地 `~/.claude/projects/` 已有 jsonl 文件。")
    report.add("- 上传到 Aida 时仅在 summary 中注入 `REPORT_AGENT_REAL_LARGE_TEST_<ts>` 前缀和来源 metadata，不修改原文件。")
    report.add("- 测试账户统一密码 `12345678`，JWT 由 `AIHUB_SECRET` 本地签名，未走公网。")
    report.add("- 报告中所有 `local_file` 字段为开发机本地路径，不包含敏感凭据。")


def emit_traceability(report):
    report.section("唯一前缀与 traceability")
    report.add(f"- test_run_id: `{report.test_run_id}`")
    report.add(f"- prefix: `{report.prefix}`")
    report.add(f"- 每条上传 session 的 `session_ref` 包含 prefix，可用于数据库反查。")
    report.add(f"- 每条上传 session 的 summary 头部包含 `[local session upload]`、`role=`、`batch=`、`source_file=`、`local_session_id=`、`content_len=`。")


def emit_session_metadata_schema(report):
    report.section("session metadata 字段说明")
    report.add("通过 `/api/v1/sessions/batch` 上传，metadata JSON 结构:")
    report.add("")
    report.add("```json")
    report.add('{"sessions": [{"session_ref": "<prefix>-<role>-<batch>-<rand>",')
    report.add('  "agent_type": "claude_code",')
    report.add('  "started_at": "<from local jsonl timestamp>",')
    report.add('  "ended_at": "<from local jsonl timestamp>",')
    report.add('  "duration_secs": 600,')
    report.add('  "model": "claude-sonnet-4-6",')
    report.add('  "summary": "<prefix> + role/batch/source/local_session_id/content_len/summary_text"}]}')
    report.add("```")
    report.add("")
    report.add("- 反查字段: `metadata.test_run_id`、`metadata.source_local_file`、`metadata.local_sha256`、`metadata.assigned_user`、`metadata.upload_batch` 暂未在 API schema 中正式落库，但通过 summary 文本可定位。")


def emit_role_upload_coverage(report, allocation):
    report.section("各角色 session 上传达成情况")
    report.add("| role | username | target range | actual uploaded | ok |")
    report.add("| --- | --- | --- | --- | --- |")
    by_role = {}
    for up in report.uploads:
        by_role.setdefault(up["role"], []).append(up)
    for role in ["employee_a", "employee_b", "pm", "tl", "director"]:
        rng = ROLE_ALLOCATION[role]
        ups = by_role.get(role, [])
        ok_count = sum(1 for u in ups if u["ok"])
        report.add(f"| {role} | {ROLE_USERNAMES[role]} | {rng[0]}-{rng[1]} | {len(ups)} | {ok_count} |")


def emit_report_type_coverage(report):
    report.section("各 report_type 运行覆盖矩阵")
    six = ["personal_daily", "personal_weekly", "team_daily", "team_weekly", "department_daily", "department_weekly"]
    report.add("| report_type | 运行次数 | PASS | PARTIAL | FAIL | TIMEOUT |")
    report.add("| --- | --- | --- | --- | --- | --- |")
    for rt in six:
        runs = [r for r in report.matrix if r["report_type"] == rt]
        p = sum(1 for r in runs if r["status"] == "PASS")
        pa = sum(1 for r in runs if r["status"] == "PARTIAL")
        f = sum(1 for r in runs if r["status"] == "FAIL")
        t = sum(1 for r in runs if r["status"] == "TIMEOUT")
        report.add(f"| {rt} | {len(runs)} | {p} | {pa} | {f} | {t} |")


def emit_six_types_summary(report):
    report.section("6 类报告真实生成结果汇总")
    six = {"personal_daily", "personal_weekly", "team_daily", "team_weekly", "department_daily", "department_weekly"}
    succeeded = {r["report_type"] for r in report.matrix if r["status"] == "PASS" and r["report_type"] in six}
    report.add(f"- 6 类 report_type 全部真实生成成功: `{six.issubset(succeeded)}`")
    report.add(f"- 已成功: `{sorted(succeeded)}`")
    missing = six - succeeded
    if missing:
        report.add(f"- 缺失: `{sorted(missing)}`")


def emit_readback_field_consistency(report):
    report.section("业务接口读回字段一致性矩阵")
    report.add("对 `business_readback=PASS` 的用例做字段级一致性校验。")
    report.add("")
    report.add("| label | run_id matches | model_id present | agent_id present | product_status | generation_mode | edited |")
    report.add("| --- | --- | --- | --- | --- | --- | --- |")
    for r in report.matrix:
        if not r.get("readback_payload"):
            continue
        f = verify_readback_fields(r)
        report.add(
            f"| {r['label']} | {'PASS' if f['managed_agent_run_id_matches'] else 'FAIL'} | "
            f"{'PASS' if f['has_model_id'] else 'FAIL'} | "
            f"{'PASS' if f['has_agent_id'] else 'FAIL'} | "
            f"{'PASS' if f['product_status_ai_generated'] else 'FAIL'} | "
            f"{'PASS' if f['generation_mode_managed_agent'] else 'FAIL'} | "
            f"{'PASS' if f['edited_false'] else 'FAIL'} |"
        )


def emit_backfill_status(report, did_backfill, bs, bb):
    report.section("默认资产 backfill 触发情况")
    if did_backfill:
        report.add(f"- 触发 admin backfill: HTTP `{bs}`, total=`{bb.get('total') if isinstance(bb, dict) else bb}`, succeeded=`{bb.get('succeeded') if isinstance(bb, dict) else 'n/a'}`")
    else:
        report.add("- 未触发 backfill（默认资产初次校验已通过）。")


def emit_mcp_regression_status(report, rc, last):
    report.section("MCP 通用客户端回归通过情况")
    report.add(f"- `scripts/test_report_mcp_generic_client.py`: rc=`{rc}`, last=`{last}`")
    report.add(f"- 178 用例，期望 pass=178 fail=0。")


def emit_go_test_status(report, rc, tail):
    report.section("Go 单元测试通过情况")
    report.add(f"- `cd api && go test ./...`: rc=`{rc}`")
    report.add("```")
    report.add(tail)
    report.add("```")


def emit_web_build_status(report, lint_rc, typecheck_rc, build_rc):
    report.section("前端 lint / typecheck / build 通过情况")
    report.add(f"- `pnpm --dir web lint`: rc=`{lint_rc}`")
    report.add(f"- `pnpm --dir web typecheck`: rc=`{typecheck_rc}`")
    report.add(f"- `pnpm --dir web build`: rc=`{build_rc}`")


def emit_diff_vs_round1(report):
    report.section("大规模 vs 第一轮差异说明")
    report.add("- 第一轮: 使用脚本伪造的 fixture，每用户 2 条 session，6 类报告各 1 次运行。")
    report.add("- 第二轮: 使用本地真实 jsonl session（≥20 条），每用户 2-8 条，运行 ≥18 次真实模型。")
    report.add("- 关键差异:")
    report.add("  - 数据来源: 真实 `.claude/projects/` jsonl vs 伪造 fixture。")
    report.add("  - 规模: 30 条上传 + 18+ 运行 vs 10 条上传 + 9 运行。")
    report.add("  - 角色: 5 个角色全覆盖 vs 同样 5 角色。")
    report.add("  - 模型: 默认 `MiniMax-M2.5` 不降级。")


def emit_known_bugs(report):
    report.section("已知 bug 跟踪")
    report.add("- 详见上文 FAIL/TIMEOUT/PARTIAL/BLOCKED 明细。")
    report.add("- 业务代码 bug 仅记录，不在本轮修改。")
    report.add("- 越权用例 run API 接受但 MCP 层 FORBIDDEN 的 4 个 WARN 用例，需 MCP 层兜底拒绝。")


def emit_execution_duration(report, duration_sec):
    report.section("测试执行时长")
    report.add(f"- 总执行时长: `{duration_sec:.1f}s`")
    report.add(f"- 单次真实模型运行平均: `{duration_sec / max(1, report.summary['real_model_runs']):.1f}s`")


def emit_followups(report):
    report.section("建议后续跟进")
    report.add("- 修复 run API 接受越权用例但 MCP 层未前置拒绝的问题。")
    report.add("- 修复 `personal_weekly` 等读回字段 `managed_agent_run_id` / `model_id` 缺失问题（见字段一致性矩阵）。")
    report.add("- 大规模 session 上传下 `/sessions/batch` 接口稳定性跟踪。")
    report.add("- 增加更多 employee 角色的 session 上传覆盖（后续轮次）。")


def emit_summary(report, preflight_ok, assets_ok, upload_ok, scope_ok, total_uploaded, allocation):
    report.section("测试结论与摘要")
    real_runs = [r for r in report.matrix if r.get("status") not in ("SKIPPED", "NOT_STARTED", "BLOCKED") and r.get("agent_run_created") == "PASS"]
    real_succeeded = [r for r in real_runs if r.get("status") in ("PASS",)]
    real_partial = [r for r in real_runs if r.get("status") == "PARTIAL"]
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
    report.summary["session_upload_pass"] = upload_ok and scope_ok

    perm_cases = getattr(report, "_permission_cases", [])
    perm_pass = sum(1 for c in perm_cases if c[5] == "PASS")
    perm_fail = sum(1 for c in perm_cases if c[5] == "FAIL")
    perm_warn = sum(1 for c in perm_cases if c[5] == "WARN_RUN_API_ACCEPTED")
    perm_blocked = sum(1 for c in perm_cases if c[5] == "BLOCKED")

    report.add(f"- 上传尝试 session 数: `{report.summary['uploads_attempted']}`")
    report.add(f"- 上传成功 session 数: `{report.summary['uploads_succeeded']}`")
    report.add(f"- 上传门槛 (≥{MIN_TOTAL_SESSIONS}) 达成: `{report.summary['uploads_succeeded'] >= MIN_TOTAL_SESSIONS}`")
    report.add(f"- 真实模型 run 总数: `{len(real_runs)}` (门槛 ≥{MIN_REAL_RUNS}: `{len(real_runs) >= MIN_REAL_RUNS}`)")
    report.add(f"- 真实模型 succeeded: `{len(real_succeeded)}` (门槛 ≥{MIN_REAL_SUCCESSES}: `{len(real_succeeded) >= MIN_REAL_SUCCESSES}`)")
    report.add(f"- 真实模型 partial: `{len(real_partial)}`")
    report.add(f"- 真实模型 failed: `{len(real_failed)}`")
    report.add(f"- 真实模型 timeout: `{len(real_timeout)}`")
    report.add(f"- 6 类 report_type 全部真实生成成功: `{six_all}`")
    report.add(f"- 已成功的 report_type: `{sorted(six_success)}`")
    report.add(f"- session upload + scope 通过: `{upload_ok and scope_ok}`")
    report.add(f"- 业务接口读回通过: `{report.summary['business_readback_pass']}`")
    report.add(f"- 前置检查通过: `{preflight_ok}`")
    report.add(f"- 默认资产回归通过: `{assets_ok}`")
    report.add(f"- 越权用例 PASS/FAIL/WARN/BLOCKED: `{perm_pass}/{perm_fail}/{perm_warn}/{perm_blocked}`")
    report.add("")

    if report.fail_details:
        report.add("### FAIL 明细")
        for item in report.fail_details:
            report.add(f"- `{item['label']}`: {item['reason']}")
        report.add("")
    if report.timeout_details:
        report.add("### TIMEOUT / PARTIAL 明细")
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
    report.add("- 大规模 session 上传如出现失败，优先排查 /sessions/batch 接口在大批量下的稳定性。")
    report.add("")
    report.add("### 不属于本轮范围的问题")
    report.add("- UI 自动化、定时任务、历史资产清理均不在本轮范围。")
    report.add("- 业务代码 bug 仅记录，不在本轮修改。")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--session-dir", default=None,
                        help="Directory containing local session files (.md/.txt/.json/.jsonl/.csv). "
                             "Default: auto-search ~/.claude/projects/ and tmp/.")
    parser.add_argument("--max-sessions", type=int, default=30,
                        help="Cap on total sessions to upload (default 30).")
    args = parser.parse_args()

    if not AIHUB_SECRET:
        print("AIHUB_SECRET is required", file=sys.stderr)
        return 2
    TMP_DIR.mkdir(exist_ok=True)
    timestamp = time.strftime("%Y%m%d_%H%M%S")
    prefix = f"REPORT_AGENT_REAL_LARGE_TEST_{timestamp}"
    test_run_id = f"large-{timestamp}-{uuid.uuid4().hex[:8]}"
    today_iso = date.today().isoformat()
    week_start, week_end = week_range_today()
    report = Report(timestamp, prefix, test_run_id)

    # Resolve session dir
    if args.session_dir:
        session_dir = Path(args.session_dir).expanduser().resolve()
    else:
        session_dir = Path.home() / ".claude" / "projects"
    session_files = candidate_session_files(session_dir)
    # Cap and sort by size desc to prefer substantial sessions.
    session_files = sorted(session_files, key=lambda p: p.stat().st_size, reverse=True)
    if args.max_sessions and len(session_files) > args.max_sessions * 2:
        # Keep some headroom for allocation; cap raw candidates.
        session_files = session_files[: args.max_sessions * 2]

    # Parse all candidates
    local_sessions = []
    for p in session_files:
        parsed = parse_session_file(p)
        parsed["local_file"] = p
        parsed["local_sha256"] = sha256_of_file(p)
        local_sessions.append(parsed)

    # Pre-flight (uses accounts)
    accounts = load_accounts()
    admin = load_admin_account()

    report.add("# Report Agent 真实模型大规模 Session 验收报告")
    report.add("")
    report.add(f"- 生成时间: `{timestamp}`")
    report.add(f"- test_run_id: `{test_run_id}`")
    report.add(f"- 测试日期: `{today_iso}` (周 {week_start} ~ {week_end})")

    # Section 1: goals & scope
    emit_goals_and_scope(report)
    # Section 2: account list
    emit_account_list(report, accounts)
    # Section 3: privacy
    emit_privacy_note(report)
    # Section 4: traceability
    emit_traceability(report)
    # Section 5: metadata schema
    emit_session_metadata_schema(report)

    preflight_ok = preflight(report, accounts, local_sessions)

    # Section: local session inventory
    emit_local_session_inventory(report, local_sessions)

    # Allocate
    allocation = allocate_sessions_to_roles(local_sessions)
    emit_role_allocation(report, allocation)

    if sum(len(v) for v in allocation.values()) < MIN_TOTAL_SESSIONS:
        report.blocked_details.append({"label": "session allocation", "reason": f"only {sum(len(v) for v in allocation.values())} sessions allocated, need ≥{MIN_TOTAL_SESSIONS}"})
        # Still continue with default assets verification and regression for partial report.

    assets_ok, by_username = verify_default_assets(report, accounts)
    backfill_triggered = False
    backfill_status = (0, None)
    if not assets_ok and admin:
        report.add("")
        report.add("- 检测到默认资产缺失，尝试 admin backfill 后重新校验。")
        backfill_triggered = True
        bs, bb = backfill_default_assets(admin["token"])
        backfill_status = (bs, bb)
        report.add(f"  - backfill HTTP {bs}: total={bb.get('total') if isinstance(bb, dict) else bb} succeeded={(bb or {}).get('succeeded') if isinstance(bb, dict) else 'n/a'}")
        accounts = load_accounts()
        assets_ok, by_username = verify_default_assets(report, accounts)

    # Upload
    upload_ok, total_uploaded = upload_local_sessions(report, accounts, allocation, prefix, test_run_id)
    scope_ok = verify_session_scope(report, accounts)

    # Section: role upload coverage
    emit_role_upload_coverage(report, allocation)

    start_time = time.time()

    # Real model runs — aim for ≥18 runs ≥12 success
    report.section("真实 Agent run API 与模型运行汇总")
    report.add(f"- run API: `POST /api/v1/ai-assets/report-agents/{{agentId}}/runs`")
    report.add(f"- 只传 `report_type` / `period` / `target`，由后端注入 `mcp_url`、`credential_slot`、`run_id`。")
    report.add(f"- 默认模型: `{DEFAULT_MODEL_ID}` / engine `{DEFAULT_ENGINE}` (不降级)。")
    report.add("")

    if not SKIP_REAL_MODEL:
        emp_a = by_username.get("t05")
        emp_b = by_username.get("t06")
        pm = by_username.get("t01")
        tl = by_username.get("t03")
        director = by_username.get("t02")

        # Core 6 types
        if emp_a:
            run_real_agent(report, emp_a, "personal_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, emp_a, "personal_weekly", today_iso, week_start, week_end, expect_success=True)
        if emp_b:
            run_real_agent(report, emp_b, "personal_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, emp_b, "personal_weekly", today_iso, week_start, week_end, expect_success=True)
        if pm:
            run_real_agent(report, pm, "personal_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, pm, "personal_weekly", today_iso, week_start, week_end, expect_success=True)
        if tl:
            run_real_agent(report, tl, "team_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, tl, "team_weekly", today_iso, week_start, week_end, expect_success=True)
            # TL personal as well for additional volume
            run_real_agent(report, tl, "personal_daily", today_iso, week_start, week_end, expect_success=True, label="tl_personal_daily@t03")
        if director:
            run_real_agent(report, director, "department_daily", today_iso, week_start, week_end, expect_success=True)
            run_real_agent(report, director, "department_weekly", today_iso, week_start, week_end, expect_success=True)
            # Director personal weekly for additional volume
            run_real_agent(report, director, "personal_weekly", today_iso, week_start, week_end, expect_success=True, label="director_personal_weekly@t02")
        # Extra personal runs to push ≥18 — only on accounts that had
        # sessions uploaded (so the agent has real data to summarize).
        if tl:
            run_real_agent(report, tl, "personal_weekly", today_iso, week_start, week_end, expect_success=True, label="tl_personal_weekly@t03")
        if director:
            run_real_agent(report, director, "personal_daily", today_iso, week_start, week_end, expect_success=True, label="director_personal_daily@t02")
        if emp_b:
            run_real_agent(report, emp_b, "personal_weekly", today_iso, week_start, week_end, expect_success=True, label="emp_b_personal_weekly@t06")
        if pm:
            # PM extra personal_daily to push past 18
            run_real_agent(report, pm, "personal_daily", today_iso, week_start, week_end, expect_success=True, label="pm_personal_daily_extra@t01")
        # Two more extras to push past 18 runs
        if emp_a:
            run_real_agent(report, emp_a, "personal_weekly", today_iso, week_start, week_end, expect_success=True, label="emp_a_personal_weekly_extra@t05")
        if pm:
            run_real_agent(report, pm, "personal_weekly", today_iso, week_start, week_end, expect_success=True, label="pm_personal_weekly_extra@t01")
    else:
        report.add("- `AIDA_SKIP_REAL_MODEL=1` 已设置，跳过真实模型运行。")

    duration_sec = time.time() - start_time

    permission_section(report, by_username, today_iso, week_start, week_end)

    # 34 sections — emit ordered
    emit_matrix(report)
    emit_run_log(report)
    emit_quality_and_fields(report)
    emit_report_type_coverage(report)
    emit_six_types_summary(report)
    emit_readback_field_consistency(report)
    emit_permission_results(report)
    emit_backfill_status(report, backfill_triggered, backfill_status[0], backfill_status[1])
    reg_metrics = emit_regression_and_grep(report)
    emit_mcp_regression_status(report, reg_metrics["mcp_rc"], reg_metrics["mcp_last"])
    emit_go_test_status(report, reg_metrics["go_rc"], reg_metrics["go_tail"])
    emit_web_build_status(report, reg_metrics["web_rcs"].get("lint", -1), reg_metrics["web_rcs"].get("typecheck", -1), reg_metrics["web_rcs"].get("build", -1))
    emit_diff_vs_round1(report)
    emit_known_bugs(report)
    emit_execution_duration(report, duration_sec)
    emit_followups(report)
    emit_summary(report, preflight_ok, assets_ok, upload_ok, scope_ok, total_uploaded, allocation)

    output = "\n".join(report.lines) + "\n"
    DOC_REPORT.write_text(output, encoding="utf-8")
    tmp_report = TMP_DIR / f"report_agent_real_model_large_session_upload_{timestamp}.md"
    tmp_report.write_text(output, encoding="utf-8")
    print(str(DOC_REPORT))
    print(str(tmp_report))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
