const page = document.querySelector("#role-page");
const drawer = document.querySelector("#drawer");
const drawerMask = document.querySelector("#drawer-mask");
const drawerClose = document.querySelector("#drawer-close");
const toast = document.querySelector("#toast");

let currentRole = "director";
let currentRange = "today";
let currentFilter = "all";
const handled = new Set();

const roleLabels = {
  director: "总监",
  pm: "PM",
  tl: "TL",
  employee: "员工"
};

const data = {
  director: {
    header: {
      eyebrow: "部门总览 / Dashboard",
      title: "部门 AI 研发看板",
      subtitle: "看部门总体、团队活跃、重点需求、跨团队阻塞、Token 趋势和部门报告。"
    },
    stats: [
      ["今日部门 Session", "187", "已上报 Session"],
      ["任务完成率", "73%", "进行中需求加权"],
      ["跨团队阻塞", "2", "1 个超过阈值"],
      ["今日 Token", "3.2M", "辅助指标"]
    ],
    reports: [
      action("dir-report-1", "部门每日进展", "今天 20:00 生成，写入飞书文档；站内只保留链接与 Markdown 预览。", "报告", "普通", "正常", 2, ["查看报告"]),
      action("dir-report-2", "部门周报 W25", "周五 17:00 生成，包含团队对比、重点需求、跨团队阻塞和 Token 趋势。", "报告", "普通", "正常", 2, ["查看报告"])
    ],
    keyRequirements: [
      ["REQ-001", "AI 平台 v3.0", "PM", "AI工程 + 推理加速", "5/7", "71%", "07-30", "8.4M", "重点关注"],
      ["REQ-003", "用户中心统一认证", "总监", "推理加速", "4/6", "67%", "06-20", "2.1M", "高关注"],
      ["REQ-005", "安全加固专项", "总监", "AI工程 + 模型训练", "1/3", "33%", "06-10", "1.2M", "高关注 + 超期"]
    ],
    blockers: [
      action("dir-block-1", "REQ-005 安全加固专项超期", "Deadline 已过，进度 33%。重点关注使其在需求总览和风险区显著展示，但不自动改变正式优先级。", "需求", "高关注", "超期", 0, ["查看需求", "补充说明"]),
      action("dir-block-2", "T-104 AI 工程 -> 推理加速接口依赖阻塞", "跨团队阻塞超过 1 天，PM 已介入。P0 仅做首页站内曝光。", "任务", "高关注", "跨团队阻塞", 0, ["查看任务", "补充说明"])
    ],
    activity: ["10:44 PM 为 T-104 添加协调结论", "10:30 总监关注 REQ-005", "09:50 部门日报草稿已生成"],
    token: ["今日 3.2M", "本周 22.5M", "本月 285M", "模型分布：Sonnet 55% / Opus 25% / GPT-5 15%"]
  },
  pm: {
    header: {
      eyebrow: "需求 / 项目视角",
      title: "PM 首页",
      subtitle: "看重点需求、缺 AC、跨团队阻塞、项目进展和 Token 按需求分布。"
    },
    stats: [
      ["重点需求", "5", "2 个高关注"],
      ["缺 AC", "2", "影响验收边界"],
      ["跨团队阻塞", "1", "超过 1 天"],
      ["本周 Token", "6.8M", "按需求辅助分析"]
    ],
    requirements: [
      action("pm-req-1", "REQ-051 智能工单归因缺 AC", "总监关注，但验收标准缺失，暂不适合进入拆任务阶段。", "需求", "高关注", "缺 AC", 0, ["补充说明", "标记已处理"]),
      action("pm-req-2", "REQ-042 统一权限模型跨团队阻塞", "AI 工程到推理加速依赖超过 1 天，TL 已补说明。", "需求", "高关注", "跨团队阻塞", 0, ["查看需求", "补充说明"]),
      action("pm-req-3", "REQ-037 日报自动汇总进展异常", "本周 Session 多，但 AC 和任务状态未推进。Token 只作为异常分析辅助。", "需求", "中关注", "进展异常", 1, ["查看需求", "补充说明"])
    ],
    table: [
      ["REQ-051", "智能工单归因", "缺 AC", "总监关注", "0/0", "今日补齐 AC"],
      ["REQ-042", "统一权限模型", "跨团队阻塞", "PM/TL 关注", "5/7", "协调 TL"],
      ["REQ-037", "日报自动汇总", "进展异常", "我关注", "3/6", "确认范围"]
    ],
    activity: ["10:30 总监关注 REQ-051", "10:02 TL 为 REQ-042 补充阻塞说明", "09:18 REQ-037 Session 多但 AC 无变化"],
    token: ["按需求：REQ-042 2.4M", "按团队：AI 工程 3.1M", "按模型：Sonnet 61%"]
  },
  tl: {
    header: {
      eyebrow: "团队 / 任务视角",
      title: "AI 工程 TL 首页",
      subtitle: "看团队任务、成员活跃、阻塞预警、日报 Review 和本队 Token。"
    },
    stats: [
      ["今日活跃", "11/13", "当日有已上报 Session"],
      ["参与需求", "4", "2 个跨团队"],
      ["阻碍预警", "1", "T-143 阻塞"],
      ["本队 Token", "1.4M", "辅助指标"]
    ],
    tasks: [
      action("tl-task-1", "T-143 跨团队接口联调阻塞", "任务属于 REQ-042，被 PM 关注。阻塞超过 1 天，需要 TL 补处理意见。", "任务", "高关注", "阻塞", 0, ["查看任务", "补充说明"]),
      action("tl-task-2", "T-128 权限回归用例缺工作证据", "缺记录不天然等于风险；该任务因高关注 + 明天到期显著展示。", "任务", "中关注", "临近 deadline", 1, ["查看任务", "补充说明"])
    ],
    reviews: [
      action("tl-report-1", "王五日报待 Review", "日报描述接口联调，但缺任务关联。", "日报", "普通", "待 Review", 1, ["Review 通过", "打回"]),
      action("tl-report-2", "赵六日报待 Review", "日报缺少明日计划。", "日报", "普通", "待 Review", 2, ["Review 通过", "打回"])
    ],
    table: [
      ["张三", "T-128", "REQ-042", "进行中", "缺 Session", "明天"],
      ["李四", "T-143", "REQ-042", "阻塞", "PM 关注", "本周五"],
      ["王五", "日报", "-", "待 Review", "缺任务关联", "今天"]
    ],
    activity: ["10:15 PM 关注 REQ-042", "09:52 王五日报缺任务关联", "09:31 张三上传 4 个 Session"],
    token: ["成员分布：张三 594K", "任务分布：T-143 420K", "团队本周 12.9M"]
  },
  employee: {
    header: {
      eyebrow: "个人 / 执行视角",
      title: "张三首页",
      subtitle: "看我的任务、待绑定 Session、待确认日报、上下游依赖和个人 Token。"
    },
    stats: [
      ["我的任务", "3", "2 个进行中"],
      ["待绑定 Session", "1", "T-128"],
      ["待确认日报", "1", "今天 20:00 前"],
      ["今日 Token", "186K", "辅助指标"]
    ],
    tasks: [
      action("emp-task-1", "T-128 权限回归用例补齐", "属于 REQ-042，被 TL 关注，明天到期。需要绑定今日相关 Session。", "任务", "高关注", "待绑定 Session", 0, ["绑定 Session", "补充说明"]),
      action("emp-task-2", "T-121 接口输出整理", "下游 T-143 等待你的输出，需求被 PM 关注。", "任务", "高关注", "依赖等待", 0, ["更新状态", "补充说明"]),
      action("emp-task-3", "确认今日个人日报", "日报草稿已生成，待本人确认。", "日报", "普通", "待确认", 1, ["确认日报", "补充说明"])
    ],
    sessions: [
      ["sess_8fa2", "09:10-10:22", "T-128", "高", "86K", "待绑定"],
      ["sess_8fb9", "11:00-11:48", "T-121", "中", "54K", "已匹配"],
      ["sess_8fc1", "14:05-14:36", "未匹配", "低", "46K", "待确认"]
    ],
    activity: ["09:42 TL 刘关注 T-128", "09:10 T-143 标记等待你的输出", "08:50 个人日报草稿已生成"],
    token: ["今日 186K", "本周 594K", "任务维度：T-128 86K"]
  }
};

function action(id, title, desc, type, focus, status, priority, actions) {
  return { id, title, desc, type, focus, status, priority, actions };
}

function header(role) {
  const h = data[role].header;
  return `
    <header class="role-header role-${role}">
      <div>
        <p class="eyebrow">${h.eyebrow}</p>
        <h1>${h.title}</h1>
        <p class="page-subtitle">${h.subtitle}</p>
      </div>
      <div class="topbar-actions">
        <div class="segmented">
          ${["today", "week", "month"].map((r) => `<button class="segment ${currentRange === r ? "is-active" : ""}" data-range="${r}">${r === "today" ? "今日" : r === "week" ? "本周" : "本月"}</button>`).join("")}
        </div>
        <select id="focus-filter">
          <option value="all" ${currentFilter === "all" ? "selected" : ""}>全部</option>
          <option value="high" ${currentFilter === "high" ? "selected" : ""}>高关注</option>
          <option value="highRisk" ${currentFilter === "highRisk" ? "selected" : ""}>高关注且异常</option>
        </select>
      </div>
    </header>
  `;
}

function stats(role) {
  return `<section class="role-stats">${data[role].stats.map(([label, value, hint]) => `
    <div class="role-stat"><span>${label}</span><strong>${value}</strong><em>${hint}</em></div>
  `).join("")}</section>`;
}

function filter(items) {
  if (!items) return [];
  if (currentFilter === "high") return items.filter((i) => i.focus === "高关注");
  if (currentFilter === "highRisk") return items.filter((i) => i.focus === "高关注" && i.priority === 0);
  return items;
}

function card(item) {
  const done = handled.has(item.id);
  return `
    <article class="role-card ${item.priority === 0 ? "is-hot" : item.priority === 1 ? "is-warm" : ""}">
      <div>
        <div class="role-card-title">${item.title}</div>
        <p>${item.desc}</p>
        <div class="item-meta">
          <span class="chip info">${item.type}</span>
          <span class="chip ${item.focus === "高关注" ? "high" : "medium"}">${item.focus}</span>
          <span class="chip ${item.priority === 0 ? "high" : "medium"}">${item.status}</span>
          ${done ? '<span class="chip success">已处理</span>' : ""}
        </div>
      </div>
      <div class="role-card-actions">
        <button class="mini-button" data-open="${item.id}">查看</button>
        ${(item.actions || []).slice(0, 2).map((a) => `<button class="mini-button" data-action="${a}" data-id="${item.id}">${a}</button>`).join("")}
      </div>
    </article>
  `;
}

function cards(items) {
  const list = filter(items).filter((i) => !handled.has(i.id));
  return list.map(card).join("") || '<div class="empty-state">当前筛选下没有事项。</div>';
}

function table(rows, headers) {
  return `
    <table class="role-table">
      <thead><tr>${headers.map((h) => `<th>${h}</th>`).join("")}</tr></thead>
      <tbody>${rows.map((r) => `<tr>${r.map((c) => `<td>${c}</td>`).join("")}</tr>`).join("")}</tbody>
    </table>
  `;
}

function side(role) {
  const d = data[role];
  return `
    <aside class="role-aside">
      <section class="role-panel quiet-panel">
        <h2>最近动态</h2>
        <ul class="activity-mini">${d.activity.map((a) => `<li>${a}</li>`).join("")}</ul>
      </section>
      <section class="role-panel usage-panel">
        <h2>Token / Session 辅助信息</h2>
        <div class="usage-pills">${d.token.map((t) => `<span>${t}</span>`).join("")}</div>
        <p>仅作为工作证据和趋势背景，不作为首页主叙事。</p>
      </section>
    </aside>
  `;
}

function directorPage() {
  const d = data.director;
  return `
    ${header("director")}
    ${stats("director")}
    <section class="director-layout">
      <section class="role-panel decision-board">
        <div class="section-title"><span>01</span><h2>重点需求总览</h2></div>
        ${table(d.keyRequirements, ["需求", "标题", "创建者", "团队", "AC", "进度", "Deadline", "本周 Token", "关注"])}
      </section>
      <section class="role-panel project-board">
        <div class="section-title"><span>02</span><h2>跨团队阻塞 / 风险</h2></div>
        ${cards(d.blockers)}
      </section>
      <section class="role-panel">
        <div class="section-title"><span>03</span><h2>部门报告</h2></div>
        ${cards(d.reports)}
      </section>
      ${side("director")}
    </section>
  `;
}

function pmPage() {
  const d = data.pm;
  return `
    ${header("pm")}
    ${stats("pm")}
    <section class="pm-layout">
      <section class="role-panel governance-board">
        <div class="section-title"><span>01</span><h2>重点需求 / 缺 AC / 阻塞</h2></div>
        ${cards(d.requirements)}
      </section>
      <section class="role-panel requirement-board">
        <div class="section-title"><span>02</span><h2>需求列表摘要</h2></div>
        ${table(d.table, ["需求", "标题", "状态", "关注", "AC", "PM 下一步"])}
      </section>
      ${side("pm")}
    </section>
  `;
}

function tlPage() {
  const d = data.tl;
  return `
    ${header("tl")}
    ${stats("tl")}
    <section class="tl-layout">
      <section class="role-panel war-room">
        <div class="section-title"><span>01</span><h2>团队任务 / 阻塞</h2></div>
        ${cards(d.tasks)}
      </section>
      <section class="role-panel review-lane">
        <div class="section-title"><span>02</span><h2>日报 Review</h2></div>
        ${cards(d.reviews)}
      </section>
      <section class="role-panel team-board">
        <div class="section-title"><span>03</span><h2>成员任务状态</h2></div>
        ${table(d.table, ["成员", "对象", "需求", "状态", "关注/证据", "截止"])}
      </section>
      ${side("tl")}
    </section>
  `;
}

function employeePage() {
  const d = data.employee;
  return `
    ${header("employee")}
    ${stats("employee")}
    <section class="employee-layout">
      <div class="employee-inbox">
        <section class="role-panel action-panel">
          <div class="section-title"><span>01</span><h2>我的任务 / 日报待处理</h2></div>
          ${cards(d.tasks)}
        </section>
        <section class="role-panel">
          <div class="section-title"><span>02</span><h2>我的 Session 上报</h2></div>
          ${table(d.sessions, ["Session", "时间", "AI 匹配任务", "置信度", "Token", "状态"])}
        </section>
      </div>
      ${side("employee")}
    </section>
  `;
}

function render() {
  const pages = { director: directorPage, pm: pmPage, tl: tlPage, employee: employeePage };
  page.innerHTML = pages[currentRole]();
}

function allItems(role) {
  return Object.values(data[role]).filter(Array.isArray).flat().filter((x) => x && x.id);
}

function findItem(id) {
  return allItems(currentRole).find((i) => i.id === id);
}

function openDrawer(item) {
  document.querySelector("#drawer-type").textContent = `${roleLabels[currentRole]} · ${item.type}`;
  document.querySelector("#drawer-title").textContent = item.title;
  document.querySelector("#drawer-content").innerHTML = `
    <div class="drawer-section">
      <h3>对象说明</h3>
      <p>${item.desc}</p>
      <div class="item-meta">
        <span class="chip ${item.focus === "高关注" ? "high" : "medium"}">${item.focus}</span>
        <span class="chip ${item.priority === 0 ? "high" : "medium"}">${item.status}</span>
        <span class="chip info">P0 站内曝光</span>
      </div>
    </div>
    <div class="drawer-section">
      <h3>P0 承接边界</h3>
      <p>这里是最小详情 / 抽屉承接，只验证首页点击后的上下文查看和轻量动作，不代表完整业务闭环。</p>
    </div>
    <div class="drawer-section">
      <h3>补充说明</h3>
      <textarea placeholder="输入 mock 说明"></textarea>
    </div>
  `;
  document.querySelector("#drawer-actions").innerHTML = item.actions.map((a) => `<button class="${a.includes("打回") ? "danger-button" : "primary-button"}" data-action="${a}" data-id="${item.id}">${a}</button>`).join("");
  drawer.classList.add("is-open");
  drawer.setAttribute("aria-hidden", "false");
  drawerMask.hidden = false;
}

function closeDrawer() {
  drawer.classList.remove("is-open");
  drawer.setAttribute("aria-hidden", "true");
  drawerMask.hidden = true;
}

function showToast(message) {
  toast.textContent = message;
  toast.hidden = false;
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => (toast.hidden = true), 1800);
}

document.addEventListener("click", (event) => {
  const roleTab = event.target.closest("[data-role]");
  if (roleTab) {
    currentRole = roleTab.dataset.role;
    handled.clear();
    document.querySelectorAll(".role-tab").forEach((tab) => tab.classList.toggle("is-active", tab === roleTab));
    render();
    return;
  }

  const range = event.target.closest("[data-range]");
  if (range) {
    currentRange = range.dataset.range;
    render();
    return;
  }

  const open = event.target.closest("[data-open]");
  if (open) {
    const item = findItem(open.dataset.open);
    if (item) openDrawer(item);
    return;
  }

  const actionButton = event.target.closest("[data-action]");
  if (actionButton) {
    if (["标记已处理", "确认日报", "Review 通过"].includes(actionButton.dataset.action)) {
      handled.add(actionButton.dataset.id);
    }
    showToast(`已模拟：${actionButton.dataset.action}`);
    closeDrawer();
    render();
  }
});

document.addEventListener("change", (event) => {
  if (event.target.id === "focus-filter") {
    currentFilter = event.target.value;
    render();
  }
});

drawerClose.addEventListener("click", closeDrawer);
drawerMask.addEventListener("click", closeDrawer);
document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") closeDrawer();
});

render();
