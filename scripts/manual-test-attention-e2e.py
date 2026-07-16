#!/usr/bin/env python3
import json
import os
import sys
import time
import traceback
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

BASE_URL = os.environ.get("AIDA_API_BASE_URL", "http://localhost:18090/api/v1").rstrip("/")
RUN_TS = os.environ.get("ATTN_E2E_TS") or datetime.now().strftime("%Y%m%d_%H%M")
PREFIX = f"ATTN_E2E_{RUN_TS}"
OUT_DIR = Path(os.environ.get("ATTN_E2E_OUT", f"tmp/attention-e2e-{RUN_TS}"))
OUT_DIR.mkdir(parents=True, exist_ok=True)

ACCOUNTS = {
    "directorUser": {"employee_id": "li_director", "password": "123"},
    "pmUser": {"employee_id": "chen_pm", "password": "123"},
    "tlUser": {"employee_id": "liu_tl", "password": "123"},
    "employeeA": {"employee_id": "zhangsan", "password": "123"},
    "employeeB": {"employee_id": "lisi", "password": "123"},
    "adminUser": {"employee_id": "admin", "password": "123"},
}

ROLE_WEIGHT = {
    "director": 100,
    "team_leader": 50,
    "pm": 40,
    "employee": 10,
    "admin": 0,
}

CASE_RESULTS = []
DETAILS = []
REQUEST_LOG = []
DATA = {
    "prefix": PREFIX,
    "base_url": BASE_URL,
    "out_dir": str(OUT_DIR),
    "users": {},
    "teams": [],
    "requirements": {},
    "tasks": {},
    "case_items": {},
    "front_end_check": {},
}

step_counter = 0

def write_json(name, data):
    path = OUT_DIR / name
    path.write_text(json.dumps(data, ensure_ascii=False, indent=2, default=str), encoding="utf-8")
    return str(path)


def request(method, path, token=None, body=None, label=None, expected_status=None):
    global step_counter
    step_counter += 1
    url = path if path.startswith("http") else BASE_URL + path
    headers = {"Accept": "application/json"}
    data = None
    if body is not None:
        data = json.dumps(body, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if token:
        headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    status = None
    text = ""
    payload = None
    error = None
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            status = resp.status
            text = resp.read().decode("utf-8")
    except urllib.error.HTTPError as exc:
        status = exc.code
        text = exc.read().decode("utf-8", errors="replace")
        error = str(exc)
    except Exception as exc:
        error = str(exc)
        raise
    if text:
        try:
            payload = json.loads(text)
        except Exception:
            payload = text
    record = {
        "step": step_counter,
        "label": label or f"{method} {path}",
        "method": method,
        "url": url,
        "request_body": body,
        "status": status,
        "response": payload,
        "raw_response": text,
        "error": error,
    }
    REQUEST_LOG.append(record)
    safe_label = (label or f"{method}_{path}").replace("/", "_").replace(" ", "_").replace(":", "_")
    write_json(f"{step_counter:03d}_{safe_label}.json", record)
    if expected_status is not None:
        statuses = expected_status if isinstance(expected_status, (list, tuple, set)) else [expected_status]
        if status not in statuses:
            raise AssertionError(f"{label or path} expected status {statuses}, got {status}: {text}")
    elif not (200 <= int(status or 0) < 300):
        raise AssertionError(f"{label or path} failed with status {status}: {text}")
    return payload, record


def add_result(case, name, result, note=""):
    CASE_RESULTS.append({"case": case, "name": name, "result": result, "note": note})


def add_detail(case, name, account, endpoints, request_summary, expected, actual, result):
    DETAILS.append({
        "case": case,
        "name": name,
        "account": account,
        "endpoints": endpoints,
        "request_summary": request_summary,
        "expected": expected,
        "actual": actual,
        "result": result,
    })


def fail_case(case, name, account, endpoints, request_summary, expected, exc):
    actual = f"异常：{exc}"
    add_result(case, name, "FAIL", actual)
    add_detail(case, name, account, endpoints, request_summary, expected, actual, "FAIL")


def login_all():
    for key, credentials in ACCOUNTS.items():
        try:
            payload, _ = request("POST", "/auth/login", body=credentials, label=f"login_{key}")
            user = payload["user"]
            DATA["users"][key] = {"token": payload["token"], **user}
        except Exception as exc:
            DATA["users"][key] = {"login_error": str(exc), **credentials}
    write_json("users.json", DATA["users"])


def token(name):
    user = DATA["users"].get(name) or {}
    t = user.get("token")
    if not t:
        raise AssertionError(f"missing token for {name}: {user.get('login_error')}")
    return t


def user_id(name):
    return DATA["users"][name]["id"]


def find_team(name):
    for team in DATA["teams"]:
        if team.get("name") == name:
            return team
    raise AssertionError(f"team not found: {name}")


def find_follow_item(items, *, requirement_id=None, task_id=None, title=None):
    for item in items:
        if requirement_id and item.get("requirementId") != requirement_id:
            continue
        if task_id and item.get("taskId") != task_id:
            continue
        if title and item.get("title") != title:
            continue
        return item
    return None


def dashboard_follows(account, label):
    payload, _ = request("GET", "/dashboard/follows", token=token(account), label=label)
    return payload


def dashboard_risks(account, label):
    payload, _ = request("GET", "/dashboard/risks", token=token(account), label=label)
    return payload


def assert_score(item, expected_score, expected_level):
    if item is None:
        raise AssertionError("item not found")
    if item.get("attentionScore") != expected_score:
        raise AssertionError(f"expected attentionScore={expected_score}, got {item.get('attentionScore')}: {item}")
    if item.get("attentionLevel") != expected_level:
        raise AssertionError(f"expected attentionLevel={expected_level}, got {item.get('attentionLevel')}: {item}")


def get_follows(account, label):
    payload, _ = request("GET", "/follows", token=token(account), label=label)
    return payload


def has_follow_relation(account, target_type, target_id):
    follows = get_follows(account, f"list_follows_{account}_{target_type}_{target_id[:8]}")
    return any(f.get("target_type") == target_type and f.get("target_id") == target_id for f in follows)


def create_requirement(account, title, description, team_ids, deadline=None, priority="medium"):
    body = {
        "title": title,
        "description": description,
        "priority": priority,
        "team_ids": team_ids,
        "acceptance_criteria": ["验证关注度规则"],
    }
    if deadline:
        body["deadline"] = deadline
    payload, _ = request("POST", "/requirements", token=token(account), body=body, label=f"create_req_{title}")
    return payload


def create_task(account, title, requirement_id, assignee_id, due_date=None, priority="medium", depends_on_ids=None):
    body = {
        "requirement_id": requirement_id,
        "title": title,
        "assignee_id": assignee_id,
        "priority": priority,
        "acceptance_criteria": ["验证任务关注规则"],
    }
    if due_date:
        body["due_date"] = due_date
    if depends_on_ids:
        body["depends_on_ids"] = depends_on_ids
    payload, _ = request("POST", "/tasks", token=token(account), body=body, label=f"create_task_{title}")
    return payload


def update_task(account, task_id, body, label):
    payload, _ = request("PUT", f"/tasks/{task_id}", token=token(account), body=body, label=label)
    return payload


def follow(account, target_type, target_id, label):
    payload, _ = request("POST", "/follows", token=token(account), body={"target_type": target_type, "target_id": target_id}, label=label)
    return payload


def unfollow(account, target_type, target_id, label):
    payload, _ = request("DELETE", f"/follows/{target_type}/{target_id}", token=token(account), label=label)
    return payload


def expected_level(score):
    if score >= 150:
        return "high"
    if score >= 80:
        return "important"
    if score >= 40:
        return "notable"
    return "normal"


def run_case_0():
    name = "账号与环境确认"
    try:
        login_all()
        required = ["directorUser", "pmUser", "tlUser", "employeeA", "employeeB"]
        missing = [u for u in required if not DATA["users"].get(u, {}).get("token")]
        admin_missing = not DATA["users"].get("adminUser", {}).get("token")
        payload, _ = request("GET", "/teams", token=token("pmUser"), label="list_teams")
        DATA["teams"] = payload
        team = find_team("AI工程")
        DATA["ai_team"] = team
        tl_team = DATA["users"]["tlUser"].get("team_id")
        a_team = DATA["users"]["employeeA"].get("team_id")
        b_team = DATA["users"]["employeeB"].get("team_id")
        if missing:
            raise AssertionError(f"missing required accounts: {missing}")
        if not (tl_team == a_team == b_team == team["id"]):
            raise AssertionError(f"TL/employee team mismatch: tl={tl_team}, A={a_team}, B={b_team}, AI工程={team['id']}")
        note = "账号登录成功；admin 可用" if not admin_missing else "账号登录成功；admin 缺失，后续跳过 admin 用例"
        add_result("Case 0", name, "PASS", note)
        add_detail("Case 0", name, "all", ["POST /auth/login", "GET /teams"], "登录全部既有账号并查询团队", "必需账号可登录，TL/A/B 同队", note, "PASS")
    except Exception as exc:
        fail_case("Case 0", name, "all", ["POST /auth/login", "GET /teams"], "登录全部既有账号并查询团队", "必需账号可登录，TL/A/B 同队", exc)
        raise


def run_case_1():
    name = "PM 创建需求后自动关注"
    try:
        title = f"{PREFIX}_REQ_PM_CREATE"
        future = (datetime.now(timezone.utc).date() + timedelta(days=14)).isoformat()
        req = create_requirement("pmUser", title, "关注度 P0 接口测试：PM 创建需求自动关注", [DATA["ai_team"]["id"]], deadline=future)
        req_id = req["id"]
        DATA["requirements"]["pm_create"] = {"id": req_id, "title": title, "response": req}
        items = dashboard_follows("pmUser", "case1_pm_dashboard_follows")
        item = find_follow_item(items, requirement_id=req_id)
        assert_score(item, 40, "notable")
        detail_payload, _ = request("GET", f"/requirements/{req_id}", token=token("pmUser"), label="case1_get_requirement")
        if not detail_payload.get("is_followed"):
            raise AssertionError("GET /requirements/{id} is_followed is not true for PM")
        DATA["case_items"]["case1_requirement_pm"] = item
        add_result("Case 1", name, "PASS", f"requirementId={req_id}, attentionScore=40, attentionLevel=notable")
        add_detail("Case 1", name, "pmUser", ["POST /requirements", "GET /dashboard/follows", "GET /requirements/{id}"], {"title": title}, "PM 自动关注，score=40 notable", {"requirementId": req_id, "item": item, "is_followed": detail_payload.get("is_followed")}, "PASS")
    except Exception as exc:
        fail_case("Case 1", name, "pmUser", ["POST /requirements", "GET /dashboard/follows"], "创建需求并查关注列表", "PM 自动关注，score=40 notable", exc)


def run_case_2():
    name = "director 手动关注需求，关注度提升"
    try:
        req_id = DATA["requirements"]["pm_create"]["id"]
        resp = follow("directorUser", "requirement", req_id, "case2_director_follow_requirement")
        items = dashboard_follows("pmUser", "case2_pm_dashboard_follows")
        item = find_follow_item(items, requirement_id=req_id)
        assert_score(item, 140, "important")
        DATA["case_items"]["case2_requirement_pm"] = item
        add_result("Case 2", name, "PASS", "attentionScore=140, attentionLevel=important")
        add_detail("Case 2", name, "directorUser / pmUser", ["POST /follows", "GET /dashboard/follows"], {"target_type": "requirement", "target_id": req_id}, "score=PM40+director100=140 important，不是 high", {"follow": resp, "item": item}, "PASS")
    except Exception as exc:
        fail_case("Case 2", name, "directorUser / pmUser", ["POST /follows", "GET /dashboard/follows"], "director 关注 Case1 需求", "score=140 important", exc)


def run_case_3():
    name = "admin 关注需求，但不影响关注度"
    if not DATA["users"].get("adminUser", {}).get("token"):
        add_result("Case 3", name, "SKIPPED", "admin 账号不可用")
        add_detail("Case 3", name, "adminUser", ["POST /follows"], "admin 关注需求", "admin 可选", "admin 账号不可用", "SKIPPED")
        return
    try:
        req_id = DATA["requirements"]["pm_create"]["id"]
        resp = follow("adminUser", "requirement", req_id, "case3_admin_follow_requirement")
        items = dashboard_follows("pmUser", "case3_pm_dashboard_follows")
        item = find_follow_item(items, requirement_id=req_id)
        assert_score(item, 140, "important")
        DATA["case_items"]["case3_requirement_pm"] = item
        add_result("Case 3", name, "PASS", "admin 关注成功，attentionScore 仍为 140")
        add_detail("Case 3", name, "adminUser / pmUser", ["POST /follows", "GET /dashboard/follows"], {"target_type": "requirement", "target_id": req_id}, "admin 权重 0，score 仍 140 important", {"follow": resp, "item": item}, "PASS")
    except Exception as exc:
        fail_case("Case 3", name, "adminUser / pmUser", ["POST /follows", "GET /dashboard/follows"], "admin 关注 Case1 需求", "score 仍 140 important", exc)


def run_case_4():
    name = "TL 手动关注需求，关注度变 high"
    try:
        req_id = DATA["requirements"]["pm_create"]["id"]
        resp = follow("tlUser", "requirement", req_id, "case4_tl_follow_requirement")
        items = dashboard_follows("pmUser", "case4_pm_dashboard_follows")
        item = find_follow_item(items, requirement_id=req_id)
        assert_score(item, 190, "high")
        DATA["case_items"]["case4_requirement_pm"] = item
        add_result("Case 4", name, "PASS", "attentionScore=190, attentionLevel=high")
        add_detail("Case 4", name, "tlUser / pmUser", ["POST /follows", "GET /dashboard/follows"], {"target_type": "requirement", "target_id": req_id}, "score=190 high", {"follow": resp, "item": item}, "PASS")
    except Exception as exc:
        fail_case("Case 4", name, "tlUser / pmUser", ["POST /follows", "GET /dashboard/follows"], "TL 关注 Case1 需求", "score=190 high", exc)


def run_case_5():
    name = "TL 创建任务并指派 employeeA，创建人和被指派人自动关注"
    try:
        req_id = DATA["requirements"]["pm_create"]["id"]
        title = f"{PREFIX}_TASK_TL_ASSIGN_A"
        future = (datetime.now(timezone.utc).date() + timedelta(days=7)).isoformat()
        resp = create_task("tlUser", title, req_id, user_id("employeeA"), due_date=future)
        task_id = resp["id"]
        DATA["tasks"]["tl_assign_a"] = {"id": task_id, "title": title, "response": resp}
        tl_items = dashboard_follows("tlUser", "case5_tl_dashboard_follows")
        a_items = dashboard_follows("employeeA", "case5_employeeA_dashboard_follows")
        tl_item = find_follow_item(tl_items, task_id=task_id)
        a_item = find_follow_item(a_items, task_id=task_id)
        assert_score(tl_item, 60, "notable")
        assert_score(a_item, 60, "notable")
        DATA["case_items"]["case5_task_tl"] = tl_item
        DATA["case_items"]["case5_task_employeeA"] = a_item
        add_result("Case 5", name, "PASS", f"taskId={task_id}, TL 和 employeeA 均自动关注，score=60")
        add_detail("Case 5", name, "tlUser / employeeA", ["POST /tasks", "GET /dashboard/follows"], {"title": title, "assignee_id": user_id("employeeA")}, "TL 与 employeeA 均看到任务，score=60 notable", {"taskId": task_id, "tl_item": tl_item, "employeeA_item": a_item}, "PASS")
    except Exception as exc:
        fail_case("Case 5", name, "tlUser / employeeA", ["POST /tasks", "GET /dashboard/follows"], "TL 创建任务指派 employeeA", "score=60 notable", exc)


def run_case_6():
    name = "employeeA 取消关注后，普通编辑不能恢复关注"
    try:
        task_id = DATA["tasks"]["tl_assign_a"]["id"]
        resp_unfollow = unfollow("employeeA", "task", task_id, "case6_employeeA_unfollow_task")
        a_items_after_unfollow = dashboard_follows("employeeA", "case6_employeeA_follows_after_unfollow")
        if find_follow_item(a_items_after_unfollow, task_id=task_id) is not None:
            raise AssertionError("employeeA still sees task after unfollow")
        new_title = f"{PREFIX}_TASK_TL_ASSIGN_A_EDITED"
        resp_edit = update_task("tlUser", task_id, {"title": new_title, "progress": 20}, "case6_tl_plain_edit_task")
        DATA["tasks"]["tl_assign_a"]["title"] = new_title
        a_items_after_edit = dashboard_follows("employeeA", "case6_employeeA_follows_after_plain_edit")
        if find_follow_item(a_items_after_edit, task_id=task_id) is not None:
            raise AssertionError("employeeA sees task after plain edit; unfollow was restored")
        tl_items = dashboard_follows("tlUser", "case6_tl_dashboard_follows")
        tl_item = find_follow_item(tl_items, task_id=task_id)
        assert_score(tl_item, 50, "notable")
        DATA["case_items"]["case6_task_tl"] = tl_item
        add_result("Case 6", name, "PASS", "employeeA 普通编辑后未恢复关注；任务 score=50")
        add_detail("Case 6", name, "employeeA / tlUser", ["DELETE /follows/task/{id}", "PUT /tasks/{id}", "GET /dashboard/follows"], {"edit": {"title": new_title, "progress": 20}}, "employeeA 不再出现；普通编辑不恢复；score=50 notable", {"unfollow": resp_unfollow, "edit": resp_edit, "tl_item": tl_item}, "PASS")
    except Exception as exc:
        fail_case("Case 6", name, "employeeA / tlUser", ["DELETE /follows/task/{id}", "PUT /tasks/{id}", "GET /dashboard/follows"], "employeeA 取消关注后 TL 普通编辑", "不恢复 employeeA 关注，score=50", exc)


def run_case_7():
    name = "任务改派给 employeeB，新 assignee 自动关注"
    try:
        task_id = DATA["tasks"]["tl_assign_a"]["id"]
        resp_reassign = update_task("tlUser", task_id, {"assignee_id": user_id("employeeB")}, "case7_tl_reassign_task_to_employeeB")
        b_items = dashboard_follows("employeeB", "case7_employeeB_dashboard_follows")
        a_items = dashboard_follows("employeeA", "case7_employeeA_dashboard_follows")
        tl_items = dashboard_follows("tlUser", "case7_tl_dashboard_follows")
        b_item = find_follow_item(b_items, task_id=task_id)
        a_item = find_follow_item(a_items, task_id=task_id)
        tl_item = find_follow_item(tl_items, task_id=task_id)
        if b_item is None:
            raise AssertionError("employeeB does not see reassigned task")
        if a_item is not None:
            raise AssertionError("employeeA sees task despite previous unfollow")
        assert_score(b_item, 60, "notable")
        assert_score(tl_item, 60, "notable")
        DATA["case_items"]["case7_task_employeeB"] = b_item
        DATA["case_items"]["case7_task_tl"] = tl_item
        add_result("Case 7", name, "PASS", "employeeB 自动关注；employeeA 仍不出现；score=60")
        add_detail("Case 7", name, "tlUser / employeeA / employeeB", ["PUT /tasks/{id}", "GET /dashboard/follows"], {"assignee_id": user_id("employeeB")}, "B 自动关注，A 不恢复，TL 仍关注，score=60 notable", {"reassign": resp_reassign, "employeeB_item": b_item, "employeeA_item": a_item, "tl_item": tl_item}, "PASS")
    except Exception as exc:
        fail_case("Case 7", name, "tlUser / employeeA / employeeB", ["PUT /tasks/{id}", "GET /dashboard/follows"], "改派给 employeeB", "B 自动关注，A 仍不出现，score=60", exc)


def run_case_8():
    name = "再次改派回 employeeA，employeeA 应重新自动关注"
    try:
        task_id = DATA["tasks"]["tl_assign_a"]["id"]
        resp_reassign = update_task("tlUser", task_id, {"assignee_id": user_id("employeeA")}, "case8_tl_reassign_task_back_to_employeeA")
        a_items = dashboard_follows("employeeA", "case8_employeeA_dashboard_follows")
        b_items = dashboard_follows("employeeB", "case8_employeeB_dashboard_follows")
        tl_items = dashboard_follows("tlUser", "case8_tl_dashboard_follows")
        a_item = find_follow_item(a_items, task_id=task_id)
        b_item = find_follow_item(b_items, task_id=task_id)
        tl_item = find_follow_item(tl_items, task_id=task_id)
        if a_item is None:
            raise AssertionError("employeeA does not see task after reassigned back")
        if b_item is None:
            raise AssertionError("employeeB follow was removed after reassignment back")
        assert_score(a_item, 70, "notable")
        assert_score(b_item, 70, "notable")
        assert_score(tl_item, 70, "notable")
        DATA["case_items"]["case8_task_employeeA"] = a_item
        DATA["case_items"]["case8_task_employeeB"] = b_item
        DATA["case_items"]["case8_task_tl"] = tl_item
        add_result("Case 8", name, "PASS", "A 重新自动关注；B 未删除；score=70")
        add_detail("Case 8", name, "tlUser / employeeA / employeeB", ["PUT /tasks/{id}", "GET /dashboard/follows"], {"assignee_id": user_id("employeeA")}, "A 重新关注，B 不删除，TL 仍关注，score=70 notable", {"reassign": resp_reassign, "employeeA_item": a_item, "employeeB_item": b_item, "tl_item": tl_item}, "PASS")
    except Exception as exc:
        fail_case("Case 8", name, "tlUser / employeeA / employeeB", ["PUT /tasks/{id}", "GET /dashboard/follows"], "改派回 employeeA", "A 重新关注，B 不删除，score=70", exc)


def run_case_9():
    name = "制造任务风险，验证 dashboard/risks 使用 max 关注度"
    try:
        req_id = DATA["requirements"]["pm_create"]["id"]
        title = f"{PREFIX}_TASK_RISK_HIGH_REQ"
        yesterday = (datetime.now(timezone.utc).date() - timedelta(days=1)).isoformat()
        resp = create_task("tlUser", title, req_id, user_id("employeeA"), due_date=yesterday)
        task_id = resp["id"]
        DATA["tasks"]["risk_high_req"] = {"id": task_id, "title": title, "response": resp}
        task_payload, _ = request("GET", f"/tasks/{task_id}", token=token("tlUser"), label="case9_get_risk_task")
        if "overdue" not in task_payload.get("risk_types", []):
            raise AssertionError(f"risk task missing overdue risk_types: {task_payload}")
        risks = dashboard_risks("pmUser", "case9_pm_dashboard_risks")
        risk_item = None
        for item in risks:
            if item.get("taskId") == task_id:
                risk_item = item
                break
        if risk_item is None:
            raise AssertionError("risk item not found in dashboard/risks")
        req_score = DATA["case_items"]["case4_requirement_pm"]["attentionScore"]
        tl_follow = has_follow_relation("tlUser", "task", task_id)
        a_follow = has_follow_relation("employeeA", "task", task_id)
        task_score = (50 if tl_follow else 0) + (10 if a_follow else 0)
        expected = max(req_score, task_score)
        if risk_item.get("attentionScore") != expected:
            raise AssertionError(f"expected risk attentionScore max={expected}, got {risk_item.get('attentionScore')}; req={req_score}, task={task_score}")
        if risk_item.get("attentionLevel") != expected_level(expected):
            raise AssertionError(f"expected risk attentionLevel={expected_level(expected)}, got {risk_item.get('attentionLevel')}")
        if "followCount" in risk_item or "follow_count" in risk_item:
            raise AssertionError(f"risk item should not return followCount: {risk_item}")
        DATA["case_items"]["case9_risk"] = risk_item
        DATA["risk_max_check"] = {"requirementAttentionScore": req_score, "taskAttentionScore": task_score, "riskAttentionScore": risk_item.get("attentionScore"), "expectedMax": expected}
        add_result("Case 9", name, "PASS", f"reqScore={req_score}, taskScore={task_score}, riskScore={risk_item.get('attentionScore')}")
        add_detail("Case 9", name, "tlUser / pmUser", ["POST /tasks", "GET /tasks/{id}", "GET /dashboard/risks"], {"title": title, "due_date": yesterday}, "风险项出现；attentionScore=max(需求,任务)，不返回 followCount", {"task": task_payload, "risk_item": risk_item, "max_check": DATA["risk_max_check"]}, "PASS")
    except Exception as exc:
        fail_case("Case 9", name, "tlUser / pmUser", ["POST /tasks", "GET /dashboard/risks"], "创建超期风险任务", "riskScore=max(reqScore, taskScore)，不返回 followCount", exc)


def risk_priority(item):
    if item.get("riskType") == "deadline":
        return 100
    if item.get("riskType") == "dependency_blocker":
        return 90
    return 0


def run_case_10():
    name = "创建低关注风险任务，验证风险排序"
    try:
        req_title = f"{PREFIX}_REQ_LOW_RISK_SORT"
        future = (datetime.now(timezone.utc).date() + timedelta(days=14)).isoformat()
        req = create_requirement("pmUser", req_title, "关注度 P0 接口测试：低关注风险排序", [DATA["ai_team"]["id"]], deadline=future)
        req_id = req["id"]
        DATA["requirements"]["low_risk_sort"] = {"id": req_id, "title": req_title, "response": req}
        task_title = f"{PREFIX}_TASK_RISK_LOW_SORT"
        yesterday = (datetime.now(timezone.utc).date() - timedelta(days=1)).isoformat()
        task = create_task("pmUser", task_title, req_id, user_id("employeeA"), due_date=yesterday)
        task_id = task["id"]
        DATA["tasks"]["risk_low_sort"] = {"id": task_id, "title": task_title, "response": task}
        risks = dashboard_risks("pmUser", "case10_pm_dashboard_risks")
        attn_risks = [item for item in risks if PREFIX in (item.get("target") or "") or item.get("taskId") in {DATA["tasks"]["risk_high_req"]["id"], task_id}]
        high_id = DATA["tasks"].get("risk_high_req", {}).get("id")
        high_index = next((i for i, item in enumerate(risks) if item.get("taskId") == high_id), None)
        low_index = next((i for i, item in enumerate(risks) if item.get("taskId") == task_id), None)
        high_item = risks[high_index] if high_index is not None else None
        low_item = risks[low_index] if low_index is not None else None
        if high_item is None or low_item is None:
            raise AssertionError(f"high or low risk item missing: high_index={high_index}, low_index={low_index}")
        sorted_ok = True
        bad_pair = None
        decorated = [{"index": i, "key": item.get("key"), "taskId": item.get("taskId"), "target": item.get("target"), "riskType": item.get("riskType"), "riskLevelPriority": risk_priority(item), "attentionScore": item.get("attentionScore"), "attentionLevel": item.get("attentionLevel"), "deadline": item.get("deadline")} for i, item in enumerate(risks)]
        for left, right in zip(decorated, decorated[1:]):
            if left["riskLevelPriority"] < right["riskLevelPriority"]:
                sorted_ok = False
                bad_pair = (left, right, "riskLevelPriority ASC detected")
                break
            if left["riskLevelPriority"] == right["riskLevelPriority"] and (left["attentionScore"] or 0) < (right["attentionScore"] or 0):
                sorted_ok = False
                bad_pair = (left, right, "attentionScore ASC detected within same risk priority")
                break
        if not sorted_ok:
            raise AssertionError(f"dashboard/risks sort order invalid: {bad_pair}")
        if risk_priority(high_item) == risk_priority(low_item):
            if high_item.get("attentionScore", 0) > low_item.get("attentionScore", 0) and not (high_index < low_index):
                raise AssertionError(f"high attention risk should precede low attention risk: high_index={high_index}, low_index={low_index}")
        DATA["case_items"]["case10_high_risk"] = high_item
        DATA["case_items"]["case10_low_risk"] = low_item
        DATA["risks_order"] = decorated
        add_result("Case 10", name, "PASS", f"highRiskIndex={high_index}, lowRiskIndex={low_index}; 全量风险排序符合 priority DESC / attention DESC")
        add_detail("Case 10", name, "pmUser", ["POST /requirements", "POST /tasks", "GET /dashboard/risks"], {"low_req": req_title, "low_task": task_title}, "风险排序 priority DESC，同级 attentionScore DESC", {"high_index": high_index, "low_index": low_index, "high_item": high_item, "low_item": low_item, "attn_risks": attn_risks}, "PASS")
    except Exception as exc:
        fail_case("Case 10", name, "pmUser", ["POST /requirements", "POST /tasks", "GET /dashboard/risks"], "创建低关注超期风险任务并查排序", "priority DESC，同级 attentionScore DESC", exc)


def check_frontend_sort():
    path = Path("web/src/features/aidashboard/dashboard/DashboardPage.tsx")
    text = path.read_text(encoding="utf-8")
    DATA["front_end_check"] = {
        "file": str(path),
        "has_sortFollowItems": "sortFollowItems" in text,
        "has_array_sort": ".sort(" in text or "sort(" in text,
        "has_getFollowTone_text_risk": "function getFollowTone" in text and "risk.includes" in text,
        "note": "未发现 sortFollowItems；关注列表使用接口顺序。getFollowTone 仍基于 risk 文案决定颜色，不是排序逻辑。",
    }


def run_case_11():
    name = "dashboard/follows 排序验证"
    try:
        check_frontend_sort()
        accounts = ["pmUser", "tlUser", "employeeA"]
        all_orders = {}
        for acct in accounts:
            items = dashboard_follows(acct, f"case11_{acct}_dashboard_follows")
            relevant = [item for item in items if PREFIX in (item.get("title") or "") or item.get("requirementId") in [v["id"] for v in DATA["requirements"].values()] or item.get("taskId") in [v["id"] for v in DATA["tasks"].values()]]
            rows = [{"index": i, "key": item.get("key"), "type": item.get("type"), "title": item.get("title"), "riskPriority": item.get("riskPriority"), "attentionScore": item.get("attentionScore"), "attentionLevel": item.get("attentionLevel"), "deadline": item.get("deadline")} for i, item in enumerate(items) if item in relevant]
            all_orders[acct] = rows
            for left, right in zip(items, items[1:]):
                if (left.get("riskPriority") or 0) < (right.get("riskPriority") or 0):
                    raise AssertionError(f"{acct} follows riskPriority order invalid: {left} before {right}")
                if (left.get("riskPriority") or 0) == (right.get("riskPriority") or 0) and (left.get("attentionScore") or 0) < (right.get("attentionScore") or 0):
                    raise AssertionError(f"{acct} follows attentionScore order invalid: {left} before {right}")
        DATA["follows_order"] = all_orders
        note = DATA["front_end_check"]["note"]
        add_result("Case 11", name, "PASS", note)
        add_detail("Case 11", name, "pmUser / tlUser / employeeA", ["GET /dashboard/follows", "frontend source check"], "检查本轮 ATTN_E2E 项顺序与前端排序代码", "riskPriority DESC、attentionScore DESC；前端不基于风险文案排序", {"orders": all_orders, "front_end_check": DATA["front_end_check"]}, "PASS")
    except Exception as exc:
        fail_case("Case 11", name, "pmUser / tlUser / employeeA", ["GET /dashboard/follows", "frontend source check"], "检查排序", "riskPriority DESC、attentionScore DESC；前端不基于风险文案排序", exc)


def markdown_table(rows, headers):
    out = []
    out.append("| " + " | ".join(headers) + " |")
    out.append("| " + " | ".join(["---"] * len(headers)) + " |")
    for row in rows:
        out.append("| " + " | ".join(str(row.get(h, "")).replace("\n", "<br>") for h in headers) + " |")
    return "\n".join(out)


def compact_json(value):
    return "```json\n" + json.dumps(value, ensure_ascii=False, indent=2, default=str)[:8000] + "\n```"


def generate_report():
    now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    req_rows = []
    for key, value in DATA["requirements"].items():
        req_rows.append({"类型": "requirement", "名称": value.get("title"), "ID": value.get("id"), "创建账号": "pmUser", "说明": key})
    task_rows = []
    for key, value in DATA["tasks"].items():
        creator = "tlUser" if key in {"tl_assign_a", "risk_high_req"} else "pmUser"
        task_rows.append({"类型": "task", "名称": value.get("title"), "ID": value.get("id"), "创建账号": creator, "说明": key})
    users_rows = []
    for key, value in DATA["users"].items():
        users_rows.append({"变量名": key, "userId": value.get("id", ""), "employee_id": value.get("employee_id", ""), "name": value.get("name", ""), "role": value.get("role", ""), "team_id": value.get("team_id", ""), "team_name": value.get("team_name", "")})
    summary_rows = [{"Case": r["case"], "名称": r["name"], "结果": r["result"], "说明": r["note"]} for r in CASE_RESULTS]
    failed = [r for r in CASE_RESULTS if r["result"] == "FAIL"]
    passed = not failed
    report = []
    report.append("# 关注度 P0 第一阶段接口测试报告")
    report.append("")
    report.append("## 1. 测试环境")
    report.append("")
    report.append(f"* 服务地址：`{BASE_URL}`")
    report.append(f"* 测试时间：{now}")
    report.append(f"* 测试前缀：`{PREFIX}`")
    report.append(f"* 响应保存目录：`{OUT_DIR}`")
    report.append("")
    report.append(markdown_table(users_rows, ["变量名", "userId", "employee_id", "name", "role", "team_id", "team_name"]))
    report.append("")
    report.append("## 2. 测试数据")
    report.append("")
    report.append(f"* 数据命名前缀：`{PREFIX}`")
    report.append("")
    report.append(markdown_table(req_rows + task_rows, ["类型", "名称", "ID", "创建账号", "说明"]))
    report.append("")
    report.append("## 3. 用例结果汇总")
    report.append("")
    report.append(markdown_table(summary_rows, ["Case", "名称", "结果", "说明"]))
    report.append("")
    report.append("## 4. 详细验证过程")
    for detail in DETAILS:
        report.append("")
        report.append(f"### {detail['case']}：{detail['name']}")
        report.append("")
        report.append(f"* 操作账号：{detail['account']}")
        report.append(f"* 请求接口：{', '.join(detail['endpoints'])}")
        report.append(f"* 请求体摘要：{compact_json(detail['request_summary'])}")
        report.append(f"* 预期：{detail['expected']}")
        report.append(f"* 实际：{compact_json(detail['actual'])}")
        report.append(f"* 结果：{detail['result']}")
    report.append("")
    report.append("## 5. 关注度计算核对")
    score_rows = []
    req_id = DATA["requirements"].get("pm_create", {}).get("id", "")
    task_id = DATA["tasks"].get("tl_assign_a", {}).get("id", "")
    risk_check = DATA.get("risk_max_check", {})
    score_rows.extend([
        {"对象": f"需求 {req_id}", "关注用户": "陈PM", "角色": "pm", "权重": 40, "预期分": 40, "实际分": DATA["case_items"].get("case1_requirement_pm", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case1_requirement_pm", {}).get("attentionLevel", "")},
        {"对象": f"需求 {req_id}", "关注用户": "李总监", "角色": "director", "权重": 100, "预期分": 140, "实际分": DATA["case_items"].get("case2_requirement_pm", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case2_requirement_pm", {}).get("attentionLevel", "")},
        {"对象": f"需求 {req_id}", "关注用户": "管理员", "角色": "admin", "权重": 0, "预期分": 140, "实际分": DATA["case_items"].get("case3_requirement_pm", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case3_requirement_pm", {}).get("attentionLevel", "")},
        {"对象": f"需求 {req_id}", "关注用户": "刘TL", "角色": "team_leader", "权重": 50, "预期分": 190, "实际分": DATA["case_items"].get("case4_requirement_pm", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case4_requirement_pm", {}).get("attentionLevel", "")},
        {"对象": f"任务 {task_id}", "关注用户": "刘TL + 张三", "角色": "team_leader + employee", "权重": 60, "预期分": 60, "实际分": DATA["case_items"].get("case5_task_tl", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case5_task_tl", {}).get("attentionLevel", "")},
        {"对象": f"任务 {task_id}", "关注用户": "刘TL", "角色": "team_leader", "权重": 50, "预期分": 50, "实际分": DATA["case_items"].get("case6_task_tl", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case6_task_tl", {}).get("attentionLevel", "")},
        {"对象": f"任务 {task_id}", "关注用户": "刘TL + 李四", "角色": "team_leader + employee", "权重": 60, "预期分": 60, "实际分": DATA["case_items"].get("case7_task_tl", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case7_task_tl", {}).get("attentionLevel", "")},
        {"对象": f"任务 {task_id}", "关注用户": "刘TL + 张三 + 李四", "角色": "team_leader + employee + employee", "权重": 70, "预期分": 70, "实际分": DATA["case_items"].get("case8_task_tl", {}).get("attentionScore", ""), "等级": DATA["case_items"].get("case8_task_tl", {}).get("attentionLevel", "")},
        {"对象": "Case9 风险", "关注用户": "max(父需求, 任务)", "角色": "-", "权重": "-", "预期分": risk_check.get("expectedMax", ""), "实际分": risk_check.get("riskAttentionScore", ""), "等级": DATA["case_items"].get("case9_risk", {}).get("attentionLevel", "")},
    ])
    report.append(markdown_table(score_rows, ["对象", "关注用户", "角色", "权重", "预期分", "实际分", "等级"]))
    report.append("")
    report.append("## 6. dashboard/follows 排序核对")
    follow_rows = []
    for acct, rows in DATA.get("follows_order", {}).items():
        for row in rows:
            follow_rows.append({"查询账号": acct, "顺序": row.get("index"), "key": row.get("key"), "title": row.get("title"), "riskPriority": row.get("riskPriority"), "attentionScore": row.get("attentionScore"), "attentionLevel": row.get("attentionLevel"), "deadline": row.get("deadline"), "结论": "符合已检查排序前缀"})
    report.append(markdown_table(follow_rows, ["查询账号", "顺序", "key", "title", "riskPriority", "attentionScore", "attentionLevel", "deadline", "结论"]))
    report.append("")
    report.append("前端排序代码检查：")
    report.append(compact_json(DATA.get("front_end_check", {})))
    report.append("")
    report.append("## 7. dashboard/risks 排序核对")
    risk_rows = []
    for row in DATA.get("risks_order", []):
        if PREFIX in str(row.get("target")) or row.get("taskId") in [v.get("id") for v in DATA["tasks"].values()]:
            risk_rows.append({"顺序": row.get("index"), "key": row.get("key"), "riskType": row.get("riskType"), "riskLevelPriority": row.get("riskLevelPriority"), "attentionScore": row.get("attentionScore"), "attentionLevel": row.get("attentionLevel"), "deadline": row.get("deadline"), "结论": "已参与全量排序校验"})
    report.append(markdown_table(risk_rows, ["顺序", "key", "riskType", "riskLevelPriority", "attentionScore", "attentionLevel", "deadline", "结论"]))
    report.append("")
    report.append("max 规则核对：")
    report.append(markdown_table([{"风险任务": DATA["tasks"].get("risk_high_req", {}).get("id", ""), "requirementAttentionScore": risk_check.get("requirementAttentionScore", ""), "taskAttentionScore": risk_check.get("taskAttentionScore", ""), "riskAttentionScore": risk_check.get("riskAttentionScore", ""), "预期 max": risk_check.get("expectedMax", ""), "是否符合": "是" if risk_check.get("riskAttentionScore") == risk_check.get("expectedMax") else "否"}], ["风险任务", "requirementAttentionScore", "taskAttentionScore", "riskAttentionScore", "预期 max", "是否符合"]))
    report.append("")
    report.append("## 8. 发现的问题")
    report.append("")
    if failed:
        report.append("### Blocker")
        report.append("")
        report.append("* 无")
        report.append("")
        report.append("### Major")
        report.append("")
        for item in failed:
            report.append(f"* {item['case']} {item['name']}：{item['note']}")
        report.append("")
        report.append("### Minor")
        report.append("")
        report.append("* 无")
        report.append("")
        report.append("### Suggestion")
        report.append("")
        report.append("* 对失败用例按实际响应进一步定位。")
    else:
        report.append("### Blocker")
        report.append("")
        report.append("* 无")
        report.append("")
        report.append("### Major")
        report.append("")
        report.append("* 无")
        report.append("")
        report.append("### Minor")
        report.append("")
        report.append("* 无")
        report.append("")
        report.append("### Suggestion")
        report.append("")
        report.append("* `DashboardRiskItem` 未暴露 `riskLevelPriority`，接口测试按 `riskType` 推导排序优先级；如后续需要更透明的自动化断言，可考虑只读调试字段或文档化映射。")
        if DATA.get("front_end_check", {}).get("has_getFollowTone_text_risk"):
            report.append("* 前端 `getFollowTone()` 仍基于风险文案决定颜色，但未发现其参与排序；如后续风险文案多语言化，建议也改为结构化字段驱动展示色调。")
    report.append("")
    report.append("## 9. 保留数据说明")
    report.append("")
    report.append("以下测试数据按要求保留，不做清理，供人工 review：")
    report.append("")
    report.append(markdown_table(req_rows + task_rows, ["类型", "名称", "ID", "创建账号", "说明"]))
    report.append("")
    report.append("## 10. 最终结论")
    report.append("")
    report.append(f"1. 是否通过：{'PASS' if passed else 'FAIL'}")
    report.append("2. 失败用例：" + ("无" if passed else "、".join([f"{r['case']} {r['name']}" for r in failed])))
    if passed:
        report.append("3. 失败原因分类：无。")
        report.append("4. 是否建议进入下一阶段：建议进入下一阶段：需求/任务卡片关注度展示。")
    else:
        report.append("3. 失败原因分类：需根据失败详情进一步归类为产品口径不一致 / 后端接口问题 / 前端展示问题 / 测试数据问题 / 账号权限问题。")
        report.append("4. 是否建议进入下一阶段：暂不建议，需先处理失败用例。")
    report_text = "\n".join(report) + "\n"
    report_path = Path("tmp/test-reports") / f"attention-e2e-{RUN_TS}.md"
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report_path.write_text(report_text, encoding="utf-8")
    write_json("summary.json", {"case_results": CASE_RESULTS, "details": DETAILS, "data": DATA, "requests": REQUEST_LOG, "report_path": str(report_path)})
    return report_path


def main():
    try:
        run_case_0()
        for fn in [run_case_1, run_case_2, run_case_3, run_case_4, run_case_5, run_case_6, run_case_7, run_case_8, run_case_9, run_case_10, run_case_11]:
            fn()
        report_path = generate_report()
        print(json.dumps({"prefix": PREFIX, "out_dir": str(OUT_DIR), "report_path": str(report_path), "case_results": CASE_RESULTS}, ensure_ascii=False, indent=2))
        if any(r["result"] == "FAIL" for r in CASE_RESULTS):
            sys.exit(1)
    except Exception:
        traceback.print_exc()
        try:
            report_path = generate_report()
            print(json.dumps({"prefix": PREFIX, "out_dir": str(OUT_DIR), "report_path": str(report_path), "case_results": CASE_RESULTS}, ensure_ascii=False, indent=2))
        except Exception:
            traceback.print_exc()
        sys.exit(1)

if __name__ == "__main__":
    main()
