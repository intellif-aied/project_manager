#!/usr/bin/env python3
import json
import base64
import hashlib
import hmac
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
DOC_ACCOUNTS = ROOT / "doc" / "测试账号文档.md"
DOC_REPORT = ROOT / "doc" / "默认Report配置初始化验收报告.md"
TMP_DIR = ROOT / "tmp"

API_BASE = os.getenv("AIDA_API_BASE", "http://127.0.0.1:18090/api/v1").rstrip("/")
MANAGED_AGENT_URL = os.getenv("MANAGED_AGENT_URL", "http://192.168.18.107:3081").rstrip("/")
ADMIN_TOKEN = os.getenv("AIDA_ADMIN_TOKEN", "").strip()
AIHUB_SECRET = os.getenv("AIHUB_SECRET", "").strip()
RUN_SMOKE = os.getenv("AIDA_RUN_AGENT_SMOKE", "0") == "1"

REPORT_SKILL_SLUG = os.getenv("MANAGED_AGENT_REPORT_SKILL_SLUG", "aida-report")
REPORT_SKILL_VERSION = os.getenv("MANAGED_AGENT_REPORT_SKILL_VERSION", "").strip()
if not REPORT_SKILL_VERSION:
    raise SystemExit("MANAGED_AGENT_REPORT_SKILL_VERSION is required")

REPORT_SKILL_NAME = os.getenv("MANAGED_AGENT_REPORT_SKILL_NAME", "Aida Report Skill")
REPORT_SKILL_DESCRIPTION = os.getenv("MANAGED_AGENT_REPORT_SKILL_DESCRIPTION", "Aida shared Report Skill.\nAIDA_REPORT_DEFAULT:true")
REPORT_MCP_SLUG = os.getenv("MANAGED_AGENT_REPORT_MCP_SLUG", "aida-report-mcp")
REPORT_MCP_VERSION = os.getenv("MANAGED_AGENT_REPORT_MCP_VERSION", "report-v1")
REPORT_MCP_NAME = os.getenv("MANAGED_AGENT_REPORT_MCP_NAME", "Aida Report MCP")
REPORT_MCP_DESCRIPTION = os.getenv("MANAGED_AGENT_REPORT_MCP_DESCRIPTION", "Aida generic Report MCP endpoint.\nAIDA_REPORT_DEFAULT:true")
REPORT_AGENT_NAME = os.getenv("MANAGED_AGENT_REPORT_AGENT_NAME", "报告生成 Agent")
REPORT_AGENT_DESCRIPTION = os.getenv("MANAGED_AGENT_REPORT_AGENT_DESCRIPTION", "默认报告生成 Agent。")
REPORT_MCP_SLOT = os.getenv("MANAGED_AGENT_REPORT_CREDENTIAL_SLOT", "AIDA_REPORT_MCP_AUTH")
REPORT_AGENT_MARKERS = [
    "AIDA_REPORT_AGENT:default",
    "AIDA_REPORT_AGENT_TYPES:personal_daily,personal_weekly,team_daily,team_weekly,department_daily,department_weekly",
    "AIDA_MANAGED_DEFAULT_AGENT:true",
]
REQUIRED_TOOLS = [
    "get_report_context",
    "write_report_result",
    "write_report_failure",
]
FORBIDDEN_TOOLS = [
    "get_report_evidence",
    "aida_daily_report_get_context",
    "aida_daily_report_save_draft",
    "sessions[].digest",
    "report_period_summary.days",
]
REQUIRED_SKILL_TERMS = [
    "frozen Context is the only evidence source",
    "Call get_report_context exactly once",
    '{"run_id": run_id}',
    "Do not emit progress narration between tools",
    "Follow presentation_profile",
    "smallest set of coherent workstreams",
    "every materially distinct supported outcome",
    "summary as one non-empty plain-text paragraph",
    "Git commands and metadata are trace data",
    "Do not add a fixed 重点工作 heading",
]


def request_json(method, url, token=None, payload=None, timeout=30):
    data = None
    headers = {"Accept": "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + token
    if payload is not None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
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


def request_multipart(method, url, token, fields, timeout=30):
    boundary = "----aida-default-report-assets-" + uuid.uuid4().hex
    chunks = []
    for key, value in fields.items():
        chunks.append(f"--{boundary}\r\n".encode())
        chunks.append(f'Content-Disposition: form-data; name="{key}"\r\n\r\n'.encode())
        chunks.append(str(value).encode("utf-8"))
        chunks.append(b"\r\n")
    chunks.append(f"--{boundary}--\r\n".encode())
    data = b"".join(chunks)
    headers = {
        "Accept": "application/json",
        "Authorization": "Bearer " + token,
        "Content-Type": "multipart/form-data; boundary=" + boundary,
    }
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
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


def load_accounts():
    text = DOC_ACCOUNTS.read_text(encoding="utf-8")
    role_by_id = {}
    table_match = False
    for line in text.splitlines():
        if line.startswith("| 用户 ID | username | 昵称 | Aida 角色 |"):
            table_match = True
            continue
        if table_match and line.startswith("|") and "`" not in line:
            cells = [c.strip() for c in line.strip("|").split("|")]
            if len(cells) >= 4 and cells[0].isdigit():
                role_by_id[cells[0]] = {"username": cells[1], "nickname": cells[2], "role": cells[3]}
        if table_match and line.startswith("小组配置"):
            break
    accounts = []
    token_re = re.compile(r"^\|\s*(\d+)\s*\|\s*([^|]+)\|\s*([^|]+)\|[^|]*\|[^|]*\|\s*`([^`]+)`\s*\|")
    for line in text.splitlines():
        m = token_re.match(line)
        if not m:
            continue
        user_id, username, nickname, token = m.groups()
        info = role_by_id.get(user_id, {})
        accounts.append({
            "user_id": user_id,
            "username": username.strip(),
            "nickname": nickname.strip(),
            "role": info.get("role", ""),
            "token": token.strip(),
        })
    if AIHUB_SECRET:
        for account in accounts:
            account["token"] = mint_user_token(account)
        if not any(account.get("role") == "admin" for account in accounts):
            admin = load_admin_account()
            if admin:
                admin["token"] = mint_user_token(admin)
                accounts.append(admin)
    return accounts


def load_admin_account():
    user_id = os.getenv("AIDA_ADMIN_USER_ID", "").strip()
    username = os.getenv("AIDA_ADMIN_USERNAME", "").strip()
    if user_id and username:
        return {"user_id": user_id, "username": username, "nickname": username, "role": "admin", "token": ""}
    try:
        output = subprocess.check_output(
            [
                "docker",
                "compose",
                "exec",
                "-T",
                "db",
                "psql",
                "-U",
                "aidashboard",
                "-d",
                "aidashboard",
                "-At",
                "-c",
                "SELECT id::text || '|' || COALESCE(NULLIF(username,''), id::text) FROM users WHERE aida_enabled=true AND local_enabled=true AND app_role='admin' ORDER BY id LIMIT 1;",
            ],
            cwd=ROOT,
            text=True,
            stderr=subprocess.DEVNULL,
            timeout=10,
        ).strip()
    except Exception:
        return None
    if not output or "|" not in output:
        return None
    user_id, username = output.split("|", 1)
    return {"user_id": user_id, "username": username, "nickname": username, "role": "admin", "token": ""}


def b64url(data):
    return base64.urlsafe_b64encode(data).decode("ascii").rstrip("=")


def mint_user_token(account):
    now = int(time.time())
    header = {"typ": "JWT", "alg": "HS256"}
    payload = {
        "uid": int(account["user_id"]),
        "user_id": account["user_id"],
        "username": account["username"],
        "iat": now,
        "exp": now + 86400,
    }
    signing_input = b64url(json.dumps(header, separators=(",", ":")).encode()) + "." + b64url(json.dumps(payload, separators=(",", ":")).encode())
    sig = hmac.new(AIHUB_SECRET.encode(), signing_input.encode(), hashlib.sha256).digest()
    return signing_input + "." + b64url(sig)


def list_assets(token):
    status, skills = request_json("GET", API_BASE + "/ai-assets/skills?scope=mine", token)
    if status >= 300:
        raise RuntimeError(f"skills list failed {status}: {skills}")
    status, mcps = request_json("GET", API_BASE + "/ai-assets/mcp?scope=mine", token)
    if status >= 300:
        raise RuntimeError(f"mcp list failed {status}: {mcps}")
    status, agents = request_json("GET", API_BASE + "/ai-assets/agents", token)
    if status >= 300:
        raise RuntimeError(f"agents list failed {status}: {agents}")
    return skills.get("skills", []), mcps.get("entries", []), agents.get("agents", [])


def get_user(token):
    status, user = request_json("GET", API_BASE + "/auth/me", token)
    if status >= 300:
        raise RuntimeError(f"auth/me failed {status}: {user}")
    return user


def get_skill_markdown(token):
    status, integration = request_json("GET", API_BASE + "/ai-assets/daily-report-integration", token)
    if status >= 300:
        raise RuntimeError(f"integration failed {status}: {integration}")
    return integration["skill"]["skill_md"], integration["mcp"]["url"]


def find_report_skill(skills):
    matches = [s for s in skills if s.get("slug") == REPORT_SKILL_SLUG and s.get("version") == REPORT_SKILL_VERSION and not s.get("archived")]
    return matches[0] if matches else None, len(matches)


def find_report_mcp(mcps):
    matches = [m for m in mcps if m.get("slug") == REPORT_MCP_SLUG and m.get("version") == REPORT_MCP_VERSION and not m.get("archived")]
    return matches[0] if matches else None, len(matches)


def agent_text(agent):
    return "\n".join([agent.get("description", ""), agent.get("instructions", ""), agent.get("start_prompt_template", "")])


def is_default_report_agent(agent):
    text = agent_text(agent)
    return all(marker in text for marker in ["AIDA_REPORT_AGENT:default", "AIDA_MANAGED_DEFAULT_AGENT:true"]) and not agent.get("archived")


def is_legacy_daily_agent(agent):
    text = agent_text(agent)
    return "AIDA_REPORT_AGENT:personal_daily" in text or agent.get("name") == "日报"


def find_report_agent(agents):
    matches = [a for a in agents if is_default_report_agent(a)]
    if matches:
        return matches[0], len(matches), False
    legacy = [a for a in agents if is_legacy_daily_agent(a) and not a.get("archived")]
    if legacy:
        return legacy[0], 0, True
    return None, 0, False


def create_skill(token, skill_md):
    return request_multipart("POST", MANAGED_AGENT_URL + "/api/skill", token, {
        "slug": REPORT_SKILL_SLUG,
        "version": REPORT_SKILL_VERSION,
        "name": REPORT_SKILL_NAME,
        "description": REPORT_SKILL_DESCRIPTION,
        "skill_md": skill_md,
    })


def create_mcp(token, mcp_url):
    payload = {
        "slug": REPORT_MCP_SLUG,
        "version": REPORT_MCP_VERSION,
        "name": REPORT_MCP_NAME,
        "description": REPORT_MCP_DESCRIPTION,
        "transport": "http",
        "url": mcp_url,
        "auth_header": "Authorization",
        "auth_scheme": "Bearer",
        "requires_credential": True,
        "credential_env": REPORT_MCP_SLOT,
    }
    return request_json("POST", API_BASE + "/ai-assets/mcp", token, payload)


def agent_payload(agent_id, owner, mcp_url):
    description = REPORT_AGENT_DESCRIPTION
    instructions = "\n".join([
        "AIDA_REPORT_DEFAULT:true",
        *REPORT_AGENT_MARKERS,
        "你是 Aida 报告执行 Agent。每次运行必须实际加载当前绑定的 aida-report Skill，并观察到对应的 Skill tool_result 后再继续。除 Skill 加载要求外，本 Prompt 不定义报告流程、来源、结构或写作规则。",
    ])
    return {
        "agent_id": agent_id or "",
        "name": REPORT_AGENT_NAME,
        "description": description,
        "engine": os.getenv("MANAGED_AGENT_DEFAULT_ENGINE", "claude-code"),
        "default_model_id": os.getenv("MANAGED_AGENT_DEFAULT_MODEL_ID", "MiniMax-M2.5"),
        "instructions": instructions,
        "start_prompt_template": "/aida-report\nrun_id={{ run_id }}",
        "credential_slots": [{"name": REPORT_MCP_SLOT, "required": True}],
        "skills": [{"owner": owner, "slug": REPORT_SKILL_SLUG, "version": REPORT_SKILL_VERSION}],
        "mcp_servers": [{
            "name": REPORT_MCP_SLUG,
            "url": mcp_url,
            "credential_slot": REPORT_MCP_SLOT,
            "auth_header": "Authorization",
            "auth_scheme": "Bearer",
            "headers": {"X-Aida-MCP-Toolset": "managed-report"},
        }],
        "mcp_bindings": [],
    }


def create_or_repair_agent(token, owner, mcp_url, agent=None):
    if agent:
        payload = agent_payload(agent.get("agent_id"), owner, mcp_url)
        return request_json("PUT", API_BASE + f"/ai-assets/agents/{agent.get('agent_id')}", token, payload)
    payload = agent_payload("", owner, mcp_url)
    return request_json("POST", API_BASE + "/ai-assets/agents", token, payload)


def direct_backfill_user(account):
    token = account["token"]
    user = get_user(token)
    owner = user.get("username") or account["username"]
    skill_md, mcp_url = get_skill_markdown(token)
    skills, mcps, agents = list_assets(token)
    skill, skill_count = find_report_skill(skills)
    mcp, mcp_count = find_report_mcp(mcps)
    agent, agent_count, legacy_agent = find_report_agent(agents)
    result = {
        "user_id": account["user_id"],
        "username": account["username"],
        "role": account["role"],
        "skill_created": False,
        "mcp_created": False,
        "agent_created": False,
        "agent_repaired": False,
        "old_personal_daily_repaired": False,
        "error": "",
    }
    if not skill:
        status, body = create_skill(token, skill_md)
        if status >= 300:
            raise RuntimeError(f"create skill failed {status}: {body}")
        result["skill_created"] = True
    if not mcp:
        status, body = create_mcp(token, mcp_url)
        if status >= 300:
            raise RuntimeError(f"create mcp failed {status}: {body}")
        result["mcp_created"] = True
    if not agent:
        status, body = create_or_repair_agent(token, owner, mcp_url, None)
        if status >= 300:
            raise RuntimeError(f"create agent failed {status}: {body}")
        result["agent_created"] = True
    elif legacy_agent:
        status, body = create_or_repair_agent(token, owner, mcp_url, agent)
        if status >= 300:
            raise RuntimeError(f"repair legacy agent failed {status}: {body}")
        result["agent_repaired"] = True
        result["old_personal_daily_repaired"] = True
    skills2, mcps2, agents2 = list_assets(token)
    _, result["skill_count"] = find_report_skill(skills2)
    _, result["mcp_count"] = find_report_mcp(mcps2)
    _, result["agent_count"], _ = find_report_agent(agents2)
    return result


def check_skill_content(account, skill):
    token = account["token"]
    owner = skill.get("owner") or account["username"]
    url = MANAGED_AGENT_URL + "/api/skill/" + urllib.parse.quote(owner or "") + "/" + urllib.parse.quote(REPORT_SKILL_SLUG) + "/" + urllib.parse.quote(REPORT_SKILL_VERSION) + "/file?path=SKILL.md"
    req = urllib.request.Request(url, headers={"Authorization": "Bearer " + token}, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            text = resp.read().decode("utf-8")
    except urllib.error.HTTPError:
        skill_md, _ = get_skill_markdown(token)
        create_skill(token, skill_md)
        req = urllib.request.Request(url, headers={"Authorization": "Bearer " + token}, method="GET")
        with urllib.request.urlopen(req, timeout=30) as resp:
            text = resp.read().decode("utf-8")
    missing = [tool for tool in REQUIRED_TOOLS if tool not in text]
    missing.extend(["term:" + term for term in REQUIRED_SKILL_TERMS if term not in text])
    forbidden = [tool for tool in FORBIDDEN_TOOLS if tool in text]
    return missing, forbidden


def inspect_user_assets(account):
    token = account["token"]
    user = get_user(token)
    skills, mcps, agents = list_assets(token)
    skill, skill_count = find_report_skill(skills)
    mcp, mcp_count = find_report_mcp(mcps)
    agent, agent_count, _ = find_report_agent(agents)
    skill_missing = []
    skill_forbidden = []
    if skill:
        try:
            skill_missing, skill_forbidden = check_skill_content(account, skill)
        except Exception as exc:
            skill_missing = [f"unable to read skill file: {exc}"]
    token_leaks = []
    if mcp:
        dumped = json.dumps(mcp, ensure_ascii=False)
        for marker in ["Bearer ", "mcp_authorization", account["token"][:12]]:
            if marker in dumped:
                token_leaks.append(marker)
    return {
        "user": user,
        "skills_total": len(skills),
        "mcp_total": len(mcps),
        "agents_total": len(agents),
        "skill_exists": bool(skill),
        "mcp_exists": bool(mcp),
        "agent_exists": bool(agent),
        "skill_count": skill_count,
        "mcp_count": mcp_count,
        "agent_count": agent_count,
        "skill_missing_tools": skill_missing,
        "skill_forbidden_tools": skill_forbidden,
        "mcp_token_leaks": token_leaks,
        "mcp": mcp,
        "agent": agent,
    }


def run_smoke(account, report_type, period, target):
    token = account["token"]
    assets = inspect_user_assets(account)
    agent = assets["agent"]
    if not agent:
        return {"status": "BLOCKED", "error": "default Report Agent missing"}
    payload = {"report_type": report_type, "period": period, "target": target}
    status, body = request_json("POST", API_BASE + f"/ai-assets/report-agents/{agent['agent_id']}/runs", token, payload, timeout=20)
    if status >= 300:
        return {"status": "FAILED", "http_status": status, "body": body}
    dumped = json.dumps(body, ensure_ascii=False)
    leak = any(s in dumped for s in ["Bearer ", account["token"][:12], "mcp_authorization"])
    return {"status": "OK", "http_status": status, "ai_run_id": body.get("id"), "agent_id": agent.get("agent_id"), "token_leak": leak}


def main():
    accounts = load_accounts()
    if not accounts:
        print("No test accounts found", file=sys.stderr)
        return 2
    TMP_DIR.mkdir(exist_ok=True)
    timestamp = time.strftime("%Y%m%d_%H%M%S")
    tmp_report = TMP_DIR / f"default_report_assets_test_result_{timestamp}.md"
    lines = ["# 默认 Report 配置初始化验收报告", ""]
    lines.append(f"- API: `{API_BASE}`")
    lines.append(f"- Managed Agent: `{MANAGED_AGENT_URL}`")
    lines.append(f"- 测试时间: `{timestamp}`")
    lines.append("")

    if ADMIN_TOKEN:
        status, body = request_json("POST", API_BASE + "/admin/ai-assets/default-report-assets/backfill", ADMIN_TOKEN)
        lines.append("## Backfill")
        lines.append("")
        lines.append(f"- 后端 backfill 状态: `{status}`")
        if status >= 300:
            lines.append(f"- 后端 backfill 错误: `{json.dumps(body, ensure_ascii=False)}`")
        else:
            lines.append(f"- total/succeeded/failed: `{body.get('total')}/{body.get('succeeded')}/{body.get('failed')}`")
        lines.append("")
    else:
        lines.append("## Backfill")
        lines.append("")
        lines.append("- `AIDA_ADMIN_TOKEN` 未提供，后端 admin backfill 调用标记为 BLOCKED。")
        lines.append("- 脚本改为使用测试账号 token 显式执行用户级直接 backfill，用于补齐当前测试账号个人资产。")
        lines.append("")
        lines.append("| user_id | username | role | skill created | mcp created | agent created | agent repaired | old personal_daily repaired | duplicate count | error |")
        lines.append("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
        for account in accounts:
            try:
                result = direct_backfill_user(account)
                dup = f"{result.get('skill_count', 0)}/{result.get('mcp_count', 0)}/{result.get('agent_count', 0)}"
                lines.append(f"| {account['user_id']} | {account['username']} | {account['role']} | {result['skill_created']} | {result['mcp_created']} | {result['agent_created']} | {result['agent_repaired']} | {result['old_personal_daily_repaired']} | {dup} |  |")
            except Exception as exc:
                lines.append(f"| {account['user_id']} | {account['username']} | {account['role']} | - | - | - | - | - | - | {str(exc)} |")
        lines.append("")

    lines.append("## AI Assets 列表与配置检查")
    lines.append("")
    lines.append("| user_id | username | role | skills | mcp | agents | report skill | report mcp | report agent | duplicate skill/mcp/agent | skill missing tools | skill forbidden tools | mcp token leak |")
    lines.append("| --- | --- | --- | ---: | ---: | ---: | --- | --- | --- | --- | --- | --- | --- |")
    inspections = {}
    for account in accounts:
        try:
            item = inspect_user_assets(account)
            inspections[account["user_id"]] = item
            dup = f"{item['skill_count']}/{item['mcp_count']}/{item['agent_count']}"
            lines.append(
                f"| {account['user_id']} | {account['username']} | {account['role']} | {item['skills_total']} | {item['mcp_total']} | {item['agents_total']} | {item['skill_exists']} | {item['mcp_exists']} | {item['agent_exists']} | {dup} | {','.join(item['skill_missing_tools'])} | {','.join(item['skill_forbidden_tools'])} | {','.join(item['mcp_token_leaks'])} |"
            )
        except Exception as exc:
            lines.append(f"| {account['user_id']} | {account['username']} | {account['role']} | - | - | - | false | false | false | - | - | - | {str(exc)} |")
    lines.append("")

    lines.append("## 删除行为")
    lines.append("")
    lines.append("- 本脚本不删除公共测试账号资产。代码层面 AI Assets 列表接口只查询，不触发默认资产创建；删除后的自动恢复只会发生在显式 backfill 或后续账号生效初始化再次执行时。")
    lines.append("")

    lines.append("## Report Agent run API smoke")
    lines.append("")
    if not RUN_SMOKE:
        lines.append("- `AIDA_RUN_AGENT_SMOKE=1` 未开启，本轮不启动真实第三方 session，避免触发模型运行。")
    else:
        today = time.strftime("%Y-%m-%d")
        smoke_cases = [
            ("307", "personal_daily", {"date": today}, {"type": "self"}),
            ("305", "team_daily", {"date": today}, {"type": "team"}),
            ("304", "department_daily", {"date": today}, {"type": "department"}),
        ]
        lines.append("| user_id | report_type | result | details |")
        lines.append("| --- | --- | --- | --- |")
        by_id = {a["user_id"]: a for a in accounts}
        for user_id, report_type, period, target in smoke_cases:
            account = by_id.get(user_id)
            if not account:
                continue
            result = run_smoke(account, report_type, period, target)
            lines.append(f"| {user_id} | {report_type} | {result.get('status')} | `{json.dumps(result, ensure_ascii=False)}` |")
    lines.append("")

    output = "\n".join(lines) + "\n"
    DOC_REPORT.write_text(output, encoding="utf-8")
    tmp_report.write_text(output, encoding="utf-8")
    print(str(DOC_REPORT))
    print(str(tmp_report))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
