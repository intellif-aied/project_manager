import type { UserRole } from "@/shared/auth/types";

import type { HelpModuleKey } from "./helpCenterConfig";

export type BusinessRole = Exclude<UserRole, "admin">;

export interface HelpScreenshot {
  alt: string;
  caption: string;
  roles?: BusinessRole[];
  src: string;
}

export interface HelpCodeBlock {
  code: string;
  label: string;
}

export interface HelpSection {
  bullets?: string[];
  codeBlocks?: HelpCodeBlock[];
  note?: string;
  paragraphs?: string[];
  roles?: BusinessRole[];
  screenshotPlaceholders?: Array<{ description: string; title: string }>;
  screenshots?: HelpScreenshot[];
  steps?: string[];
  title: string;
}

export interface HelpArticle {
  id: string;
  keywords: string[];
  lastReviewedAt?: string;
  module: HelpModuleKey;
  roles?: BusinessRole[];
  route: string;
  sections: HelpSection[];
  summary: string;
  title: string;
}

const ALL_BUSINESS_ROLES: BusinessRole[] = ["employee", "team_leader", "pm", "director"];
const MANAGEMENT_ROLES: BusinessRole[] = ["team_leader", "pm", "director"];

export const HELP_ROLE_LABELS: Record<BusinessRole, string> = {
  director: "部门总监",
  employee: "工程师",
  pm: "产品经理",
  team_leader: "团队负责人"
};

export const HELP_MODULE_LABELS: Record<HelpModuleKey, string> = {
  client: "AIDA 客户端",
  daily: "日报",
  quickstart: "快速开始",
  requirements: "需求看板",
  weekly: "周报",
  workspace: "工作台"
};

export const HELP_ARTICLES: HelpArticle[] = [
  {
    id: "quickstart-first-daily",
    module: "quickstart",
    title: "5 分钟生成第一份 AI 日报",
    summary: "从复制 Token、上传本地 Session，到在工作台生成并保存第一份日报。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["快速开始", "第一次", "Token", "安装", "login", "upload", "Session", "AI 日报"],
    sections: [
      {
        title: "完成目标",
        bullets: [
          "本机已经使用 Claude Code 或 Codex 完成过至少一段工作。",
          "将对应 Session 上传到 AIDA。",
          "在 AIDA 中生成、检查并保存第一份个人日报。"
        ],
        note: "这条流程以“日报已经保存”为完成标准，而不是只完成客户端安装或 Session 上传。"
      },
      {
        title: "1. 复制个人 Token",
        steps: ["登录 AIDA。", "点击右上角个人菜单。", "选择“复制 Token”。", "Token 只用于本人设备登录，不要发送到群聊、需求、日报或截图中。"],
        screenshots: [{ alt: "复制个人 Token 入口", caption: "从右上角个人菜单复制 Token；页面不会展示 Token 明文。", src: "/help-center/screenshots/v1/quickstart/01-copy-token-menu.png" }]
      },
      {
        title: "2. 安装 AIDA 客户端",
        paragraphs: ["根据当前操作系统下载安装 AIDA。安装后重新打开终端，并检查命令是否可用。"],
        codeBlocks: [
          { label: "Windows PowerShell 安装", code: "Invoke-RestMethod http://113.100.143.91:9180/statics-live/aida/install.ps1 | Invoke-Expression" },
          { label: "macOS / Linux 安装", code: "curl -fsSL http://113.100.143.91:9180/statics-live/aida/install.sh | bash" },
          { label: "检查客户端版本", code: "aida version" }
        ]
      },
      {
        title: "3. 登录客户端",
        steps: ["执行 aida login。", "按提示粘贴刚复制的个人 Token；服务环境已由安装脚本自动配置。", "执行 aida status，核对当前登录账号。"],
        codeBlocks: [{ label: "登录并检查状态", code: "aida login\naida status" }]
      },
      {
        title: "4. 选择并上传 Session",
        steps: [
          "执行 aida upload 进入全屏选择器，在选择器中检查项目、时间和摘要。",
          "使用方向键或 j/k 移动，按 Space 勾选一到两条安全的 Session。",
          "记录较多时按 / 输入项目、Session ID 或摘要关键词进行搜索；确认范围后按 Enter 开始上传。",
          "确认目标 Session 显示 READY；如果显示 PROCESSING，请等待服务端处理完成，不要重复启动上传。"
        ],
        codeBlocks: [{ label: "选择并上传", code: "aida upload" }],
        screenshots: [{ alt: "aida upload 全屏选择 Session", caption: "当前 AIDA 全屏选择界面。使用方向键移动、Space 勾选、/ 搜索，确认范围后按 Enter 上传；截图使用专用安全测试 Session。", src: "/help-center/screenshots/v2/quickstart/04-aida-cli-upload.png" }]
      },
      {
        title: "5. 从工作台进入日报",
        steps: ["回到 AIDA 工作台。", "找到“报告处理”。", "在“我的日报”右侧选择“填写日报”或“编辑日报”。"],
        screenshots: [{ alt: "工作台报告处理入口", caption: "工作台的报告处理区提供个人日报入口。", src: "/help-center/screenshots/v1/quickstart/02-workspace-report-entry.png" }]
      },
      {
        title: "6. 生成并保存日报",
        steps: [
          "点击日报日期旁的日历图标，选择这份日报的归属日期。这里设置的是日报日期，不是 Session 筛选日期。",
          "可以直接选择“AI 生成”；不选择 Session 时，系统会按日报日期冻结相对应的 Session 来源快照，再交给报告 Agent。",
          "如果只想总结指定工作，打开“选择 session”抽屉；抽屉里的开始日期和结束日期只用于筛选候选 Session。",
          "勾选需要的 Session 切片并关闭抽屉，再选择“AI 生成”。",
          "AI 生成完成后正文已自动保存。检查生成内容；明日计划仅由用户人工填写，修改后再选择“保存”。"
        ],
        screenshots: [
          { alt: "日报日期与 AI 生成入口", caption: "弹窗顶部的日历选择日报归属日期；AI 生成和选择 Session 是并列操作，Session 选择不是必选步骤。", src: "/help-center/screenshots/v1/dir/13-daily-ai-generate.png" },
          { alt: "Session 日期范围筛选", caption: "打开 Session 抽屉后，开始日期和结束日期只筛选候选 Session，不会修改日报日期。", src: "/help-center/screenshots/v1/dir/12-daily-session-selection.png" }
        ]
      },
      {
        title: "完成检查",
        bullets: ["“我的日报记录”出现目标日期。", "展开后可以依次查看完整正文和明日计划。", "仍可复制正文或再次编辑。"],
        screenshots: [{ alt: "日报保存后的展开结果", caption: "日报保存后会出现在个人日报记录中；展开后，明日计划显示在完整正文下方。", src: "/help-center/screenshots/v1/quickstart/03-daily-saved-result.png" }]
      }
    ]
  },
  {
    id: "quickstart-no-report-content",
    module: "quickstart",
    title: "上传后为什么没有生成内容",
    summary: "从日期、上传结果和 Session 范围三处定位 AI 日报内容为空或不准确的问题。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["没有内容", "上传成功", "日期错误", "Session", "AI 生成失败"],
    sections: [
      {
        title: "依次检查",
        steps: [
          "确认已在 aida upload 选择器中勾选目标 Session 并按 Enter，且结果显示 READY；仅打开选择器不会上传。",
          "确认日报弹窗顶部选择的是要生成的日报日期；这不是 Session 筛选条件。",
          "手动选择 Session 时，再在抽屉内使用开始日期和结束日期筛选候选记录。",
          "未手动选择 Session 时，重新执行 AI 生成，让系统按日报日期重新冻结来源快照。",
          "手动选择过 Session 时，检查是否勾选了正确切片；也可清空选择恢复按日期取数。",
          "仍无内容时执行 aida status，确认客户端登录账号与当前网页账号一致。"
        ]
      },
      {
        title: "容易误解的地方",
        note: "上传 Session 不会自动创建日报。上传只准备生成上下文，仍需在工作台或日报页面选择“AI 生成”并保存结果。"
      }
    ]
  },
  {
    id: "quickstart-client-troubleshooting",
    module: "quickstart",
    title: "登录或上传失败怎么办",
    summary: "处理找不到 aida 命令、身份不一致、本地没有 Session 和上传失败。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["找不到命令", "login 失败", "upload 失败", "status", "选择器为空"],
    sections: [
      {
        title: "快速排查",
        bullets: [
          "找不到 aida：重新执行安装，关闭终端后再打开，并用 aida version 检查。",
          "身份不正确：重新执行 aida login，并用 aida status 核对账号。",
          "上传选择器为空：确认本机已使用 Claude Code 或 Codex 产生工作记录，然后重新执行 aida upload。",
          "上传结果为 PROCESSING：服务端仍在处理，先等待再到 Session 页面确认，不要连续重复上传。",
          "上传结果为 FAILED 或 BLOCKED：保留终端错误码和发生时间，再减少选择数量重试或联系平台负责人。"
        ]
      },
      {
        title: "继续阅读",
        paragraphs: ["更完整的安装、登录、上传参数和故障说明，请进入“AIDA 客户端”栏目。"]
      }
    ]
  },
  {
    id: "client-install",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "安装或更新 AIDA 客户端",
    summary: "根据 Windows、macOS 或 Linux 安装客户端，并使用自动检查或 aida update 保持最新版本。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["安装", "更新", "自动更新", "aida update", "Windows", "PowerShell", "macOS", "Linux", "找不到命令"],
    sections: [
      {
        title: "Windows PowerShell",
        paragraphs: ["在 PowerShell 中执行下面的命令。完成后关闭终端并重新打开。"],
        codeBlocks: [
          {
            label: "Windows 安装命令",
            code: "Invoke-RestMethod http://113.100.143.91:9180/statics-live/aida/install.ps1 | Invoke-Expression"
          }
        ]
      },
      {
        title: "macOS / Linux",
        paragraphs: ["在终端中执行安装脚本。脚本会自动选择当前系统支持的客户端文件。"],
        codeBlocks: [
          {
            label: "macOS / Linux 安装命令",
            code: "curl -fsSL http://113.100.143.91:9180/statics-live/aida/install.sh | bash"
          }
        ]
      },
      {
        title: "更新客户端",
        paragraphs: ["执行 login、upload 或 status 时，AIDA 会先检查最新版本；发现新版后会校验并安装，再继续当前命令。也可以随时手动检查更新。"],
        codeBlocks: [{ label: "立即检查并更新", code: "aida update" }],
        note: "如果自动更新或 aida update 失败，请重新执行当前系统的安装命令，再用 aida version 核对版本。"
      },
      {
        title: "确认安装结果",
        codeBlocks: [
          { label: "查看版本", code: "aida version" },
          { label: "查看帮助", code: "aida help" }
        ],
        note: "如果提示找不到 aida 命令，先重新打开终端；仍无效时再检查安装输出中的 PATH 提示。"
      }
    ]
  },
  {
    id: "client-login",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "登录客户端并检查状态",
    summary: "使用个人令牌绑定 AIDA 服务，并确认当前终端登录身份正确。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["登录", "状态", "token", "令牌", "server", "身份"],
    sections: [
      {
        title: "登录",
        paragraphs: ["安装脚本已经自动配置当前 AIDA 服务环境。先在 AIDA 右上角打开个人菜单并选择“复制 Token”，然后执行下面的命令并按提示粘贴 Token。"],
        codeBlocks: [
          {
            label: "登录命令",
            code: "aida login"
          }
        ],
        note: "不要把 Token 直接写在命令参数中，避免被终端命令历史保存；按交互提示粘贴即可。"
      },
      {
        title: "核对身份",
        codeBlocks: [{ label: "查看登录状态", code: "aida status" }],
        bullets: [
          "Server 由安装脚本自动配置，普通用户无需填写或修改。",
          "User 应是你自己的姓名和角色。",
          "如果身份不符，不要继续上传，重新登录正确账号。"
        ]
      }
    ]
  },
  {
    id: "client-quick-start",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "个人设备上传 Session",
    summary: "在个人设备上交互选择或批量同步 Claude Code、Codex 工作记录。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["AIDA", "客户端", "CLI", "Session", "上传", "全屏选择", "搜索", "READY", "PROCESSING", "安装", "Claude Code", "Codex"],
    sections: [
      {
        title: "开始前确认",
        bullets: [
          "已经安装 AIDA 客户端，并通过 aida status 确认当前登录身份正确。",
          "本机已经使用 Claude Code 或 Codex 完成过工作，客户端会扫描对应的本地 Session。",
          "本页介绍个人模式：上传结果归当前登录账号。多人共用同一系统账号的开发机请改用团队模式。",
          "令牌只用于本人设备登录，不要粘贴到群聊、需求描述或截图中。"
        ]
      },
      {
        title: "选择并上传",
        steps: [
          "执行 aida upload 进入全屏选择器，并在其中确认待上传记录的项目、时间和摘要。",
          "使用方向键或 j/k 移动，按 Space 勾选；需要缩小范围时按 / 搜索。",
          "检查顶部已选择数量，确认后按 Enter 上传。"
        ],
        codeBlocks: [{ label: "交互选择并上传", code: "aida upload" }],
        note: "首次使用建议交互选择，不建议直接使用 --all；这样可以先排除无关项目或包含敏感上下文的 Session。命令 --all 仍是个人模式，不会按团队目录分发。"
      },
      {
        title: "全屏选择器操作",
        bullets: [
          "使用 ↑/↓ 或 j/k 移动光标，按 Space 勾选或取消当前 Session；页面顶部持续显示已选择数量。",
          "按 / 进入搜索，输入项目路径、Session ID、摘要或最新消息关键词；按 Enter 结束输入并保留筛选结果，按 Esc 退出搜索输入。",
          "按 a 选择或取消当前搜索结果中的全部 Session；未搜索时等于选择全部本地可发现 Session，执行前必须确认没有无关或敏感记录。",
          "确认范围后按 Enter 开始上传；按 q 或 Ctrl+C 取消且不上传。"
        ],
        screenshots: [{ alt: "aida upload 全屏选择器", caption: "当前选择器会显示项目、摘要、Session ID 和最新消息；方向键移动，Space 选择，/ 搜索，a 全选当前结果，Enter 上传。", src: "/help-center/screenshots/v2/quickstart/04-aida-cli-upload.png" }]
      },
      {
        title: "怎么看上传结果",
        paragraphs: [
          "前往 Session 页面确认记录已出现。首次成功上传会形成一个可选切片；同一 Session 有新增内容后再次上传，会只上传新增内容并形成新的独立切片。没有新增内容时客户端会跳过，上传失败或重复操作不会产生可选的重复切片。一次上传即使包含跨天内容，也仍形成一个完整切片。"
        ],
        bullets: [
          "READY：服务端已经处理完成，可以用于日报或周报。",
          "PROCESSING：上传已接收，服务端仍在处理；先等待，不要连续重复上传。",
          "CURRENT：本地 Session 仍在追加，本次已上传当前完整快照；后续有新内容时再次执行 upload。",
          "FAILED 或 BLOCKED：本次未达到可用状态，保留错误码和发生时间后重试或联系平台负责人。"
        ]
      }
    ]
  },
  {
    id: "client-team-sync",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "共享开发机使用团队同步",
    summary: "多人共用一台开发机时，按工作目录把 Session 同步到对应团队成员。",
    route: "/tokens",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["团队同步", "共享开发机", "upload --team", "auto-sync", "同步目录", "aida log", "待配置", "归属"],
    sections: [
      {
        title: "什么时候使用团队模式",
        bullets: [
          "多人共用同一个系统账号，并共同使用这台机器上的 Claude Code 或 Codex 时，使用团队模式。",
          "个人电脑或只需要上传本人 Session 时，继续使用 aida upload、aida upload --all 或个人自动同步。",
          "团队模式仍使用普通个人 Token 登录，不需要创建或保管团队账号、团队 Token。共享机上登录的账号必须属于目标团队。"
        ]
      },
      {
        title: "1. 每位成员配置自己的目录",
        steps: [
          "每位成员分别登录自己的 AIDA 网页账号，进入“我的 Token”。",
          "在日期选择器后点击文件夹图标，打开“团队同步目录”。",
          "添加本人工作目录的绝对路径，例如 /home/shared/alice/project；该目录及其子目录都会归属本人。",
          "不同成员不能配置相同或互相包含的目录；不能配置文件系统根目录 /。"
        ],
        note: "目录由成员自行维护。修改目录只影响后续团队同步；已上传 Session 不会自动迁移。删除目录后，该目录下新旧 Session 都会停止后续团队同步。",
        screenshots: [
          {
            alt: "我的 Token 页面团队同步目录入口",
            caption: "团队同步目录入口位于日期范围选择器最右侧的文件夹按钮，图中已用红色描边标出。",
            src: "/help-center/screenshots/v3/client/01-team-sync-directory-entry.png"
          }
        ]
      },
      {
        title: "2. 在共享机登录并立即同步",
        steps: [
          "共享机上的任一团队成员执行 aida login，使用自己的个人 Token 登录。",
          "执行 aida status，确认登录身份和服务地址正确。",
          "执行 aida upload --team。团队模式会扫描全部本地 Session，不会打开逐条选择器。",
          "命中成员目录的新 Session 会直接进入该成员的个人数据；既有 Session 继续沿用原归属。"
        ],
        codeBlocks: [
          { label: "团队模式立即同步", code: "aida login\naida status\naida upload --team" }
        ]
      },
      {
        title: "3. 开启团队自动同步",
        paragraphs: ["设置一次后，AIDA 会按选择的北京时间每天自动执行团队同步。"],
        codeBlocks: [
          { label: "开启团队自动同步", code: "aida auto-sync enable --team" },
          { label: "检查自动同步状态", code: "aida auto-sync status" }
        ],
        note: "普通 aida auto-sync enable 开启的是个人模式。团队模式不会由登录、升级或其他命令自动开启。"
      },
      {
        title: "4. 处理待配置目录",
        steps: [
          "如果上传结果显示“待配置”，执行 aida log 查看目录和受影响的 Session 数。",
          "让对应成员在“我的 Token”的文件夹入口补充该绝对目录。",
          "配置完成后再次执行 aida upload --team；未配置期间内容不会被上传，也不会自动归给登录者。"
        ],
        codeBlocks: [{ label: "查看团队同步待配置目录", code: "aida log" }]
      },
      {
        title: "归属规则",
        bullets: [
          "目录只决定新 Session 的个人归属；同一 Session 建立后不会因为登录账号或目录配置变化而换人。",
          "团队模式上传的 Session 直接计入归属成员的个人 Token、成本、Session 和日报统计。",
          "旧 Session 已经传错账号时不会由日常同步自动拆分，需要联系平台负责人按确认目录执行一次性历史归属迁移。",
          "目录未配置、Session 身份重复或原归属已不在当前团队时，对应 Session 不会上传；保留终端错误信息交给平台负责人处理。"
        ]
      }
    ]
  },
  {
    id: "client-auto-sync",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "设置每天自动同步",
    summary: "选择个人或团队模式，设置每天的北京时间，并检查或关闭后台同步。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["自动同步", "auto-sync", "定时", "北京时间", "enable", "set-time", "disable", "status"],
    sections: [
      {
        title: "选择同步模式",
        codeBlocks: [
          { label: "个人设备：开启个人自动同步", code: "aida auto-sync enable" },
          { label: "共享开发机：开启团队自动同步", code: "aida auto-sync enable --team" }
        ],
        note: "个人模式把 Session 上传到当前登录账号；团队模式按网页中配置的成员目录分发。两种模式需要显式选择，不会自动互相切换。"
      },
      {
        title: "管理自动同步",
        codeBlocks: [
          { label: "查看自动同步状态", code: "aida auto-sync status" },
          { label: "修改每天同步时间", code: "aida auto-sync set-time" },
          { label: "关闭自动同步", code: "aida auto-sync disable" }
        ],
        bullets: [
          "enable 和 set-time 会在终端中引导选择每天的同步时间，页面显示统一使用北京时间。",
          "也可以执行 aida status，同时检查登录、连接和自动同步状态。",
          "客户端更新后会自动恢复已开启的同步任务；需要改变个人/团队模式时，重新执行对应的 enable 命令。"
        ]
      }
    ]
  },
  {
    id: "client-additional-clients",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "同步其他 AI 编程客户端",
    summary: "检测 OpenCode、Kimi Code、OpenClaw 和 WorkBuddy，并显式选择可上传的 Session。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["clients", "upload-client", "OpenCode", "Kimi Code", "OpenClaw", "WorkBuddy", "其他客户端"],
    sections: [
      {
        title: "检测本机客户端",
        paragraphs: ["Claude Code 和 Codex 直接使用 aida upload；其他客户端先检测，再使用 upload-client。"],
        codeBlocks: [{ label: "检测支持的客户端", code: "aida clients" }]
      },
      {
        title: "上传 OpenCode 或 Kimi Code",
        codeBlocks: [
          { label: "交互选择 Session", code: "aida upload-client opencode\naida upload-client kimi_code" },
          { label: "上传全部 Session", code: "aida upload-client opencode --all\naida upload-client kimi_code --all" }
        ]
      },
      {
        title: "上传 OpenClaw",
        paragraphs: ["OpenClaw 可能包含私人或非编码对话，因此必须逐条选择，不支持 --all。"],
        codeBlocks: [
          { label: "打开选择器", code: "aida upload-client openclaw" },
          { label: "按 Session Ref 上传", code: "aida upload-client openclaw <session-ref>" }
        ],
        note: "WorkBuddy 当前只支持检测，不支持上传；以 aida clients 的检测结果为准。"
      }
    ]
  },
  {
    id: "client-session-slice-rules",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "Session 切片如何形成",
    summary: "了解首次上传、后续增量上传、跨天内容与报告选择之间的关系。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["Session", "切片", "增量上传", "跨天", "重复上传", "报告来源", "Token"],
    sections: [
      {
        title: "什么是 Session 切片",
        paragraphs: [
          "Session 切片是一次成功上传的内容范围。首次上传某个本地 Session 时，当前已有内容形成第一个切片；以后再次上传同一 Session 时，只把上次成功上传之后的新增内容形成新切片。"
        ],
        note: "切片不是“一个 Session 的全部内容”，也不是“每个自然日一个切片”。"
      },
      {
        title: "固定形成规则",
        bullets: [
          "一次成功上传最多形成一个切片；同一 Session 多次成功增量上传，可以形成多个独立切片。",
          "切片不按自然日拆分。一次上传的新增内容即使横跨多天，仍然只形成一个完整切片。",
          "没有新增内容时会跳过，不会形成新切片。",
          "上传失败不会形成可选切片；重复操作同一段内容也不会形成重复切片。",
          "每个切片的摘要只描述该切片自身包含的内容。"
        ]
      },
      {
        title: "切片如何用于个人报告",
        bullets: [
          "同一 Session 形成的多个切片，会在个人日报和个人周报的 Session 选择列表中显示为多条可独立选择的记录。",
          "手动选择切片后，已选切片是本次报告的权威 Session 来源；未选切片不会混入。",
          "候选列表的日期范围只用于筛选记录。跨天切片只要与筛选日期相交，就会作为完整切片使用，不会按日期裁剪。",
          "不手动选择切片时，系统会按日报日期或周报周期冻结默认的 Session 来源快照。"
        ]
      },
      {
        title: "与 Token 统计的区别",
        paragraphs: [
          "报告 Session 切片用于决定 AI 报告读取哪些工作内容；Token 页面则按实际使用时间统计 Token 和成本。两者用途和日期口径不同，不能用报告切片数量或列表中的 Token 直接代替 Token 统计结果。"
        ]
      }
    ]
  },
  {
    id: "client-upload-troubleshooting",
    lastReviewedAt: "2026-07-28",
    module: "client",
    title: "选择、上传与问题排查",
    summary: "掌握交互搜索、批量上传和常见失败的处理方式。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["upload", "Session", "搜索", "all", "失败", "超时", "选择器为空"],
    sections: [
      {
        title: "常用命令",
        codeBlocks: [
          { label: "交互选择并上传", code: "aida upload" },
          { label: "个人账号上传全部本地 Session", code: "aida upload --all" },
          { label: "共享机按团队目录上传全部 Session", code: "aida upload --team" },
          { label: "查看团队模式待配置目录", code: "aida log" }
        ]
      },
      {
        title: "交互模式与直接模式",
        bullets: [
          "直接执行 aida upload 会进入全屏交互选择，支持键盘移动、勾选和搜索，适合首次上传和需要核对范围的场景。",
          "aida upload --all 会按当前登录账号处理全部本地可发现 Session，不是最近一页；无变化的 Session 会跳过，但仍应先检查上传范围。",
          "aida upload --team 同样扫描全部本地 Session，但按当前团队成员配置的工作目录分发；它不会打开选择器。"
        ]
      },
      {
        title: "选择器为空",
        bullets: [
          "确认本机确实运行过 Claude Code 或 Codex，并产生了 Session。",
          "aida upload 会直接扫描全部本地可发现 Session，不需要先执行单独的列表命令。",
          "如果刚开始使用 Claude Code 或 Codex，请先完成一段真实工作并退出当前会话，再重新执行 aida upload。"
        ]
      },
      {
        title: "上传失败或超时",
        steps: [
          "先执行 aida status，确认安装配置存在且登录身份正确。",
          "确认网络可以访问 AIDA，再重试单条 Session。",
          "较大的 Session 解析时间更长；不要在上传过程中重复启动多次相同命令。",
          "仍失败时保留终端中的错误摘要和发生时间，联系平台负责人排查。"
        ]
      }
    ]
  },
  {
    id: "workspace-overview",
    module: "workspace",
    title: "从工作台开始一天的工作",
    summary: "用工作台集中查看待处理事项、风险提示和当天报告状态。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["首页", "待办", "事项", "风险", "报告处理", "Token"],
    sections: [
      {
        title: "建议查看顺序",
        steps: [
          "先查看“我的事项”，确认近期负责、关注或参与的需求与任务。",
          "再查看“我的风险提示”，优先处理逾期、依赖阻塞和依赖冲突。",
          "最后检查报告处理，确认当天日报和本周周报是否已完成。"
        ],
        screenshots: [
          { alt: "工程师工作台", caption: "工程师工作台：突出本人任务、阻塞和个人报告。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/01-workspace-overview.png" },
          { alt: "团队负责人工作台", caption: "团队负责人工作台：增加小组日报、周报处理入口。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/01-workspace-overview.png" },
          { alt: "产品经理工作台", caption: "产品经理工作台：关注跨团队需求与个人报告。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/01-workspace-overview.png" },
          { alt: "部门总监工作台", caption: "部门总监工作台：增加部门范围汇总与团队用量。", roles: ["director"], src: "/help-center/screenshots/v1/dir/01-workspace-overview.png" }
        ]
      },
      {
        title: "页面不会替你做什么",
        note: "工作台负责聚合和下钻，不替代需求看板中的完整筛选，也不替代日报、周报页面的历史记录管理。"
      }
    ]
  },
  {
    id: "workspace-items",
    module: "workspace",
    title: "查看和处理我的事项",
    summary: "理解需求与任务为何会同时出现在事项列表，并从当前页面继续处理。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["我的事项", "关注", "负责", "创建", "展开全部", "详情"],
    sections: [
      {
        title: "事项来源",
        bullets: [
          "我创建、负责或关注的需求。",
          "我负责、参与或关注的任务。",
          "与我所在团队或当前职责相关的工作项。"
        ]
      },
      {
        title: "处理流程",
        steps: [
          "查看事项的类型、阶段、截止时间和负责人摘要。",
          "选择“详情”打开需求抽屉或任务弹窗。",
          "在详情中继续查看任务、验收、依赖、风险或操作记录。",
          "列表未完整展示时，选择“展开全部”加载其余事项。"
        ]
      }
    ]
  },
  {
    id: "workspace-risks",
    module: "workspace",
    title: "判断和下钻风险",
    summary: "从风险摘要定位逾期、阻塞或依赖冲突的真实来源。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["风险", "逾期", "阻塞", "依赖", "冲突", "截止日期"],
    sections: [
      {
        title: "常见风险类型",
        bullets: [
          "需求逾期：需求仍未完成且已超过截止时间。",
          "任务逾期：任务未完成且已超过任务截止时间。",
          "依赖阻塞：上游任务未完成，当前任务无法正常推进。",
          "依赖冲突：依赖关系或时间安排存在不一致，需要负责人协调。"
        ]
      },
      {
        title: "建议动作",
        steps: [
          "选择风险对应的需求或任务。",
          "查看负责人、截止时间、依赖来源和当前进度。",
          "有编辑权限时调整任务安排；无编辑权限时联系负责人或在动态中确认变更。"
        ]
      }
    ]
  },
  {
    id: "workspace-report-processing",
    module: "workspace",
    title: "使用报告处理区",
    summary: "从工作台快速进入日报和周报，不在工作台维护全部历史记录。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["报告处理", "日报", "周报", "已保存", "暂未报告"],
    sections: [
      {
        title: "个人报告",
        bullets: [
          "“我的日报”显示当天报告状态，可直接填写或继续编辑。",
          "“我的周报”显示当前周报告状态，可直接填写或继续编辑。",
          "历史记录、成员报告和汇总报告请进入日报或周报页面。"
        ]
      },
      {
        title: "管理范围",
        roles: ["team_leader", "director"],
        bullets: [
          "团队负责人还需要关注小组成员和小组汇总报告。",
          "部门总监还需要关注部门成员和部门汇总报告。"
        ]
      }
    ]
  },
  {
    id: "workspace-role-focus",
    module: "workspace",
    title: "不同角色在工作台关注什么",
    summary: "同一工作台按职责聚合不同范围的数据，入口相同但关注重点不同。",
    route: "/dashboard",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["角色", "工程师", "团队负责人", "产品经理", "部门总监"],
    sections: [
      {
        title: "工程师",
        roles: ["employee"],
        bullets: ["优先处理本人负责的任务和依赖风险。", "确认日报、周报是否已填写。"]
      },
      {
        title: "团队负责人",
        roles: ["team_leader"],
        bullets: ["关注小组内任务分配、阻塞和逾期。", "检查小组成员报告和小组汇总报告。"]
      },
      {
        title: "产品经理",
        roles: ["pm"],
        bullets: ["关注跨团队需求、验收口径和关键风险。", "从事项下钻确认任务拆分与负责人。"]
      },
      {
        title: "部门总监",
        roles: ["director"],
        bullets: ["关注部门范围的高风险、高关注需求和报告完成情况。", "从汇总数据下钻到团队与负责人。"]
      }
    ]
  },
  {
    id: "requirements-overview",
    module: "requirements",
    title: "理解需求看板的工作流",
    summary: "需求从创建、评审和推进，到任务完成与验收的完整工作区。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["需求", "任务", "评审", "进行中", "完成", "验收"],
    sections: [
      {
        title: "完整流程",
        steps: [
          "团队负责人、产品经理或部门总监创建需求并写清目标和描述；工程师从本人可见的既有需求开始处理。",
          "有任务创建权限时，在需求详情中拆分任务，并按角色允许的人员范围设置负责人、截止时间和上游依赖。",
          "任务创建者、任务负责人或有管理权限的角色持续更新任务状态与进度。",
          "通过风险和动态跟踪逾期、阻塞及关键变更。",
          "任务完成后对照验收标准确认需求是否达到完成条件。"
        ],
        screenshots: [
          { alt: "工程师需求看板", caption: "工程师可查看本人创建、本人负责或本组参与的需求；不显示新建需求入口，创建任务时只能指派给自己。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/02-requirements-board.png" },
          { alt: "团队负责人需求看板", caption: "团队负责人可新建需求，并管理本人创建、本人负责或本组参与的需求；任务负责人限定为本组可选人员。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/02-requirements-board.png" },
          { alt: "产品经理需求看板", caption: "产品经理可查看和管理全部需求与任务，并支持跨团队分配、依赖和验收管理。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/02-requirements-board.png" },
          { alt: "部门总监需求看板", caption: "部门总监可查看和管理全部需求与任务，并从跨团队视角跟踪阶段、依赖、验收和风险。", roles: ["director"], src: "/help-center/screenshots/v1/dir/02-requirements-board.png" }
        ]
      },
      {
        title: "工程师权限边界",
        roles: ["employee"],
        bullets: ["不能新建需求。", "可查看本人创建、本人负责或本组作为参与团队的需求。", "仅能管理本人创建或本人负责的需求。", "可在本人可见的未取消需求中创建任务，但任务只能指派给自己；不能改派任务负责人。"]
      },
      {
        title: "团队负责人权限边界",
        roles: ["team_leader"],
        bullets: ["可以新建需求。", "可查看和管理本人创建、本人负责或本组作为参与团队的需求。", "创建或改派任务时，只能选择本组可用人员。", "可管理本人创建、本人负责、本组成员负责，或所属需求在本人管理范围内的任务。"]
      },
      {
        title: "产品经理与部门总监权限边界",
        roles: ["pm", "director"],
        bullets: ["可以查看、创建和管理全部需求。", "可以在未取消需求中创建和管理任务，并从全部可用业务人员中选择负责人。", "可以管理需求和任务的状态、进度、依赖与验收标准。"]
      }
    ]
  },
  {
    id: "requirements-scope-view",
    module: "requirements",
    title: "筛选范围和切换视图",
    summary: "使用范围、关键词、状态和视图切换快速找到需要处理的需求。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["我的事项", "关注", "负责", "创建", "全部", "列表", "看板", "搜索"],
    sections: [
      {
        title: "范围说明",
        bullets: [
          "我的事项：与本人创建、负责、关注或参与相关的需求。",
          "关注：本人主动关注的需求。",
          "负责：本人作为负责人之一的需求。",
          "创建：本人创建的需求。",
          "全部：当前权限范围内可查看的全部需求。"
        ]
      },
      {
        title: "选择视图",
        bullets: ["阶段看板适合按状态推进。", "需求列表适合搜索、筛选和批量浏览。"],
        screenshots: [
          { alt: "工程师需求范围与阶段看板", caption: "工程师同样可以切换我的事项、关注、负责、创建或全部；每个范围只返回本人有权查看的需求。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/02-requirements-board.png" },
          { alt: "需求范围筛选与列表视图", caption: "管理角色先选择我的事项、关注、负责、创建或全部，再根据工作场景切换阶段看板或需求列表。", roles: ["team_leader", "pm", "director"], src: "/help-center/screenshots/v1/tl/09-requirements-scope-view.png" }
        ]
      }
    ]
  },
  {
    id: "requirements-create",
    module: "requirements",
    title: "创建一条可执行的需求",
    summary: "填写标题、描述并指定负责人，再按需补充协作范围与验收口径。",
    route: "/requirements/create",
    roles: MANAGEMENT_ROLES,
    keywords: ["新建需求", "标题", "描述", "负责人", "必填", "校验", "团队", "截止", "飞书文档", "验收标准"],
    sections: [
      {
        title: "创建步骤",
        steps: [
          "在需求看板选择“新建需求”。",
          "填写标题和需求描述，描述应说明背景、目标与范围。",
          "至少选择一名负责人；负责人用于明确推进责任，不能留空。",
          "确认优先级，并按需补充参与团队、截止日期、飞书文档和可验证的验收标准。",
          "保存后进入需求详情继续拆分任务。"
        ],
        screenshots: [{ alt: "新建需求完整表单", caption: "团队负责人视角的新建需求页面；标题、描述、负责人和优先级是创建时需要重点确认的字段。", src: "/help-center/screenshots/v1/tl/10-requirements-create.png" }]
      },
      {
        title: "提交前校验",
        bullets: [
          "标题和需求描述为必填项；只输入空格不会通过校验。",
          "标题最多 120 个字符，不能包含换行、制表符或不可见控制字符。",
          "需求描述最多 10000 个字符，不能包含不可见控制字符。",
          "负责人至少选择一人；优先级必须选择低、中、高或紧急。",
          "飞书文档为可选项；填写时必须是完整的 http 或 https 链接，最长 2048 个字符。",
          "验收标准最多 50 条，每条最多 1000 个字符，不能包含不可见控制字符。"
        ],
        note: "参与团队、截止日期、飞书文档和验收标准可以留空，但应根据实际协作与交付需要及时补充。"
      },
      {
        title: "权限说明",
        note: "工程师可以查看其权限范围内的需求，但不显示新建需求入口。按钮是否可见还会受当前用户与业务数据权限影响。"
      }
    ]
  },
  {
    id: "requirements-detail",
    module: "requirements",
    title: "阅读需求详情",
    summary: "在一个抽屉中连续查看说明、负责人、团队、进度、任务、验收和动态。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["需求详情", "抽屉", "说明", "任务", "验收", "动态", "飞书文档"],
    sections: [
      {
        title: "页面结构",
        bullets: [
          "顶部：标题、阶段、优先级和主要操作。",
          "概览：需求说明、截止时间、负责人、参与团队、进度、创建者和飞书文档。",
          "任务：任务拆分、状态、负责人、截止时间和风险。",
          "验收：完成需求时需要逐项核对的标准。",
          "动态：需求与任务关键变更的时间记录。"
        ],
        screenshots: [
          { alt: "需求详情抽屉", caption: "以团队负责人视角为例：需求说明和关键字段位于概览区，任务、验收、动态及 Session 入口集中在下方工作区；操作按钮按实际权限显示。", src: "/help-center/screenshots/v1/tl/11-requirements-detail.png" }
        ]
      },
      {
        title: "快捷操作",
        bullets: ["有权限时可直接编辑截止时间、负责人和参与团队；负责人至少保留一人。", "“关联 session”位于标签栏右侧，用于管理需求级工作记录。"]
      }
    ]
  },
  {
    id: "requirements-edit-permission",
    module: "requirements",
    title: "理解需求的查看与编辑权限",
    summary: "能打开需求不代表能修改全部字段，页面会按当前需求返回的能力显示操作。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["权限", "不能编辑", "编辑按钮", "状态", "负责人", "403"],
    sections: [
      {
        title: "判断原则",
        bullets: [
          "页面是否显示编辑、阶段修改、创建任务等入口，以当前需求返回的权限为准。",
          "无权限时仍可查看需求说明、任务、验收和动态。",
          "权限不足不是数据丢失；需要修改时联系需求负责人、创建者或相应管理角色。"
        ]
      }
    ]
  },
  {
    id: "requirements-tasks",
    module: "requirements",
    title: "拆分和推进任务",
    summary: "把需求拆成可分配、可跟踪、可完成的执行任务。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["拆分任务", "添加任务", "任务负责人", "必填", "校验", "进度", "状态", "完成"],
    sections: [
      {
        title: "拆分原则",
        bullets: ["每个任务只表达一个可交付结果。", "任务标题、负责人和优先级为必填项。", "按需补充截止时间和完成标准。", "存在先后顺序时设置上游依赖。"]
      },
      {
        title: "推进步骤",
        steps: [
          "在需求详情的“任务”页签选择任务或添加任务。",
          "在任务详情中确认负责人、截止日期、进度和依赖。",
          "实际开始后更新状态和进度。",
          "遇到阻塞时先查看上游依赖和风险说明。",
          "完成后确认进度、状态和完成时间保持一致。"
        ],
        screenshots: [{ alt: "任务推进详情", caption: "任务详情集中展示负责人、截止日期、进度、状态、风险、依赖和验收标准。", src: "/help-center/screenshots/v1/tl/12-requirements-task-progress.png" }]
      },
      {
        title: "任务校验规则",
        bullets: [
          "任务标题最多 120 个字符，不能包含换行、制表符或不可见控制字符。",
          "负责人至少选择一人；优先级必须选择低、中或高。",
          "上游依赖最多选择 50 项，不能选择无权限访问或不合法的依赖。",
          "任务验收标准最多 50 条，每条最多 1000 个字符。",
          "进度必须在 0% 到 100% 之间；“阻塞”由未完成依赖和时间关系计算，不是可手动选择的任务状态。",
          "将任务设为完成时，系统同时保持完成状态、100% 进度和完成时间一致。"
        ]
      },
      {
        title: "工程师操作范围",
        roles: ["employee"],
        note: "工程师可在本人可见的未取消需求中新增只分配给自己的任务；可更新本人创建或负责的任务，也可管理本人创建或负责需求下的任务，但不能修改任务负责人。"
      },
      {
        title: "管理角色操作范围",
        roles: ["team_leader", "pm", "director"],
        note: "团队负责人只在本人或本组管理范围内拆分和推进任务，负责人限定本组；产品经理与部门总监可跨团队管理任务和负责人。"
      }
    ]
  },
  {
    id: "requirements-dependencies",
    module: "requirements",
    title: "设置依赖并处理阻塞",
    summary: "用上游依赖表达任务先后关系，并从风险信息定位阻塞来源。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["上游依赖", "依赖阻塞", "依赖冲突", "风险", "任务"],
    sections: [
      {
        title: "设置依赖",
        steps: ["确认当前任务显示依赖管理权限后，打开上游依赖选择器。", "搜索并选择必须先完成且本人有权访问的任务。", "保存后在任务详情中核对依赖关系。"],
        screenshots: [{ alt: "选择任务上游依赖", caption: "依赖选择器按需求分组展示候选任务；只选择当前任务真正需要等待的上游工作。", src: "/help-center/screenshots/v1/tl/13-requirements-dependencies.png" }]
      },
      {
        title: "处理风险",
        bullets: ["上游未完成时，当前任务可能显示依赖阻塞。", "截止时间与依赖顺序不合理时可能显示依赖冲突。", "先修正真实任务关系和时间安排，不要只处理风险标签。"]
      }
    ]
  },
  {
    id: "requirements-acceptance",
    module: "requirements",
    title: "使用验收标准",
    summary: "用可验证的条件统一需求完成口径。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["验收", "AC", "完成标准", "验收标准", "50 条", "1000 字符", "编辑冲突"],
    sections: [
      {
        title: "写法建议",
        bullets: ["每条标准只描述一个可检查结果。", "避免“体验良好”“尽量优化”等不可验证表达。", "有需求编辑权限时，范围变化后同步更新验收标准，并通过动态确认变更；只读用户负责核对，不会看到保存入口。"],
        screenshots: [{ alt: "需求验收标准页签", caption: "验收页签集中列出需求级完成条件，完成需求前应逐项核对。", src: "/help-center/screenshots/v1/tl/14-requirements-acceptance.png" }]
      },
      {
        title: "数量与内容限制",
        bullets: ["需求和任务的验收标准均为可选项。", "最多填写 50 条。", "每条最多 1000 个字符。", "不能包含不可见控制字符；空白条目保存时会被忽略。"]
      },
      {
        title: "多人编辑",
        note: "需求和任务保存时会携带当前版本。若内容已被其他人更新，页面会提示编辑冲突并刷新数据；请基于最新内容重新修改，不要覆盖他人的变更。"
      }
    ]
  },
  {
    id: "requirements-session-activity",
    module: "requirements",
    title: "关联 session 和查看动态",
    summary: "把工作记录与需求关联，并通过动态追踪关键变更。",
    route: "/requirements?scope=mine",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["关联 session", "工作记录", "动态", "操作记录", "Token"],
    sections: [
      {
        title: "关联 session",
        steps: ["打开本人有权查看的需求详情。", "在标签栏右侧选择“关联 session”。", "按时间筛选并勾选本人的工作记录。", "确认后在需求上下文中保留关联；每个用户只能新增或移除自己的 Session 关联。"],
        screenshots: [{ alt: "需求关联 Session 弹窗", caption: "通过日期范围筛选并关联可作为需求证据来源的 Session；截图仅保留经过安全检查的测试记录。", src: "/help-center/screenshots/v1/tl/15-requirements-session-link.png" }]
      },
      {
        title: "动态记录",
        bullets: ["动态按时间显示需求和任务的重要变更。", "动态用于追溯，不替代当前字段值。", "查看负责人、状态或关联记录变化时，可用动态确认是谁在何时操作。"],
        screenshots: [{ alt: "需求动态记录", caption: "动态按时间记录需求阶段、负责人和任务等关键变更，便于确认操作者与变更前后值。", src: "/help-center/screenshots/v1/tl/16-requirements-activity.png" }]
      }
    ]
  },
  {
    id: "requirements-follow",
    module: "requirements",
    title: "关注需求和理解关注度",
    summary: "用关注保存重点需求；关注度反映不同角色对需求的综合关注。",
    route: "/requirements?scope=followed",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["关注", "取消关注", "关注度", "重点需求", "星标"],
    sections: [
      {
        title: "使用方式",
        bullets: ["选择星标关注需求，再从“关注”范围集中查看。", "关注度不是任务优先级，它由关注人的角色权重聚合得出。", "取消关注不会删除需求，也不会改变需求负责人。"]
      }
    ]
  },
  {
    id: "daily-overview",
    module: "daily",
    title: "认识日报页面",
    summary: "个人记录对所有角色开放；成员视图按组织范围开放，汇总视图仍由对应负责人维护。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["日报", "我的日报", "成员日报", "小组日报", "部门日报"],
    sections: [
      {
        title: "所有角色",
        bullets: ["查看、筛选、展开和编辑个人日报记录。", "选择任意日期补写日报。", "使用 AI 生成个人日报，并可选择 session。"],
        screenshots: [
          { alt: "团队负责人个人日报", caption: "团队负责人可在个人、小组成员和小组汇总之间切换。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/03-daily-personal.png" },
          { alt: "产品经理个人日报", caption: "产品经理使用个人日报记录与编辑流程。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/03-daily-personal.png" },
          { alt: "部门总监个人日报", caption: "部门总监可在个人、部门成员和部门汇总之间切换。", roles: ["director"], src: "/help-center/screenshots/v1/dir/04-daily-personal.png" }
        ]
      },
      {
        title: "小组成员",
        roles: ["employee"],
        bullets: ["查看本人所属小组的成员日报。", "成员日报为只读视图，不开放小组汇总日报。"]
      },
      {
        title: "团队负责人",
        roles: ["team_leader"],
        bullets: ["查看小组成员日报。", "填写和维护小组汇总日报。"]
      },
      {
        title: "部门总监",
        roles: ["director"],
        bullets: ["查看部门成员日报。", "填写和维护部门汇总日报。"]
      }
    ]
  },
  {
    id: "daily-personal-records",
    module: "daily",
    title: "查看和管理我的日报记录",
    summary: "按日期筛选记录，在列表中直接阅读摘要、展开全文、复制或编辑。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["个人日报", "日期筛选", "展开", "收起", "复制全文", "明日计划", "编辑"],
    sections: [
      {
        title: "浏览记录",
        steps: ["使用开始日期和结束日期筛选历史范围。", "在折叠状态快速定位日期和记录。", "选择“展开”加载完整 Markdown 正文。", "正文下方会继续显示该日报的明日计划；没有填写时显示“未填写”。", "展开后可复制正文或再次收起。"],
        screenshots: [{ alt: "个人日报展开后的正文和明日计划", caption: "展开记录后，完整正文与明日计划在同一张记录卡片中连续展示。", src: "/help-center/screenshots/v1/dir/04-daily-personal.png" }]
      },
      {
        title: "编辑记录",
        note: "选择列表中的“编辑”会打开对应日期的日报管理弹窗。当前没有草稿机制，保存后仍可再次编辑。"
      }
    ]
  },
  {
    id: "daily-write",
    module: "daily",
    title: "填写或补写日报",
    summary: "通过统一弹窗填写任意日期的日报正文和可选明日计划。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["填写日报", "补写", "历史日期", "明日计划", "保存"],
    sections: [
      {
        title: "操作步骤",
        steps: ["选择“填写日报”。", "点击日期右侧的日历图标切换目标日期；历史日期同样可以补写。", "填写或粘贴日报正文。", "需要时填写最多两行的明日计划。", "选择“保存”，回到列表继续查看或编辑。"],
        screenshots: [{ alt: "日报内容管理弹窗", caption: "日报正文占据主要空间，明日计划保持紧凑；底部提供 AI、Session 和保存操作。", src: "/help-center/screenshots/v1/dir/05-daily-editor.png" }]
      },
      {
        title: "切换目标日期",
        paragraphs: ["个人日报和部门汇总日报的日期右侧都有日历图标。点击后选择目标日期，弹窗会加载该日期已有的报告；没有报告时可直接填写或使用 AI 生成。"],
        note: "日期切换用于从“填写日报”或“填写部门日报”打开的管理弹窗；从历史列表选择“编辑”时日期固定。若当前有未保存修改，切换前会要求确认；切换后已选 Session 也会清空，避免把其他日期的上下文带入。",
        screenshots: [
          { alt: "个人日报切换日期", caption: "点击个人日报日期旁的日历图标，可查看、补写或生成其他日期的日报。", src: "/help-center/screenshots/v1/dir/11-daily-date-switch.png" },
          { alt: "部门日报切换日期", caption: "部门汇总日报使用相同的日期切换方式，切换后处理对应日期的部门报告。", roles: ["director"], src: "/help-center/screenshots/v1/dir/14-daily-department-date-switch.png" }
        ]
      }
    ]
  },
  {
    id: "daily-ai-generate",
    module: "daily",
    title: "用 AI 生成日报",
    summary: "基于目标日期和报告上下文生成正文，再由用户检查和保存。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["AI 生成", "报告 Agent", "覆盖正文", "生成失败", "日报"],
    sections: [
      {
        title: "生成步骤",
        steps: ["打开日报管理弹窗，通过日历图标确认或切换目标日期。", "个人日报可直接选择“AI 生成”，也可先选择 session 精确指定 Session 来源。", "生成完成后正文已经自动保存；检查事实、结构和遗漏内容。", "明日计划不会由 AI 填写、修改或清空；需要时人工填写，修改正文或明日计划后再选择“保存”。"],
        screenshots: [{ alt: "日报 AI 生成入口", caption: "AI 生成与 Session 选择是并列操作；不选择 Session 也可以直接生成。", src: "/help-center/screenshots/v1/dir/13-daily-ai-generate.png" }]
      },
      {
        title: "AI 如何取得上下文",
        bullets: [
          "未选择 session：系统按目标日期冻结默认 Session 来源快照，报告 Agent 读取该快照。",
          "已选择 session：当前勾选的切片是本次报告唯一允许使用的 Session 证据，即使切片跨天或位于报告日期之外也不会被日期再次过滤。",
          "报告 Agent 仍可能读取报告日期范围内的事项、需求和已有报告；这些非 Session 业务数据不会改变已选切片的最高来源优先级。",
          "Session 选择是可选的范围控制，不是生成日报的前置条件。",
          "小组汇总日报读取本组成员已保存的个人日报，部门汇总日报读取所属小组已保存的小组日报；两者不提供个人 Session 选择，也不回退读取成员原始 Session。"
        ]
      },
      {
        title: "重要说明",
        note: "AI 生成会替换并自动保存报告正文，但不会填写、修改或清空明日计划。已有未保存内容时先确认是否继续；若当前账号尚未创建默认报告 Agent，页面会提示并在确认后直接初始化，无需跳转到 AI 资产页面。"
      }
    ]
  },
  {
    id: "daily-session-selection",
    module: "daily",
    title: "选择 session 作为日报上下文",
    summary: "个人日报可按需选择工作记录，将 AI 生成范围限制到指定 Session 切片。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["选择 session", "工作记录", "Token", "日期", "分页"],
    sections: [
      {
        title: "操作步骤",
        steps: ["在个人日报弹窗选择“选择 session”。", "默认不限制日期；需要缩小候选范围时再设置开始日期和结束日期。", "浏览切片活动时间、摘要与 Token，用复选框选择相关记录。", "列表默认每页 5 条；已选数量会立即更新，翻页不会丢失已选记录。", "关闭抽屉后选择“AI 生成”；无需额外确认按钮。"],
        screenshots: [{ alt: "选择 Session 作为日报上下文", caption: "日期筛选为可选条件；勾选需要的 Session 后，顶部会显示已选数量。截图只保留经过安全检查的测试记录。", src: "/help-center/screenshots/v1/dir/12-daily-session-selection.png" }]
      },
      {
        title: "什么时候需要选择",
        bullets: ["需要精确控制本次日报使用哪些 Session 工作证据时选择切片。", "可选择跨天或日报日期之外的切片；日期范围只筛选候选记录，不裁剪切片，也不修改日报归属日期。", "不需要限定 Session 来源时可以不选，系统会按日报日期创建默认来源快照。", "选择后可使用清空图标恢复“默认按日报日期取数”。"]
      },
      {
        title: "范围限制",
        note: "session 选择只用于个人日报；小组和部门汇总日报按组织范围聚合，不提供个人 session 选择。"
      }
    ]
  },
  {
    id: "daily-member-reports",
    module: "daily",
    title: "浏览成员日报",
    summary: "按日期快速阅读同组或部门成员日报，无需逐条打开详情页。",
    route: "/reports/daily?tab=member",
    roles: ["employee", "team_leader", "director"],
    keywords: ["成员日报", "小组成员", "部门成员", "晨会", "复制", "Markdown"],
    sections: [
      {
        title: "浏览方式",
        steps: ["切换到成员日报。", "选择需要查看的日期。", "在列表折叠状态快速浏览成员和一行内容。", "展开需要讨论的成员报告。", "需要外部使用时复制完整 Markdown 正文。"],
        screenshots: [
          { alt: "小组成员日报", caption: "团队负责人看到本组成员的提交与缺失情况。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/04-daily-members.png" },
          { alt: "部门成员日报", caption: "部门总监可按小组筛选全部部门成员日报。", roles: ["director"], src: "/help-center/screenshots/v1/dir/06-daily-members.png" }
        ]
      },
      {
        title: "数据范围",
        roles: ["employee", "team_leader"],
        note: "普通成员和团队负责人只能看到本人所属小组成员的日报。"
      },
      {
        title: "数据范围",
        roles: ["director"],
        note: "部门总监看到部门成员日报。"
      }
    ]
  },
  {
    id: "daily-team-summary",
    module: "daily",
    title: "填写小组汇总日报",
    summary: "团队负责人汇总小组当天进展、风险和后续计划。",
    route: "/reports/daily?tab=team",
    roles: ["team_leader"],
    keywords: ["小组日报", "小组汇总", "团队负责人", "填写小组日报"],
    sections: [
      {
        title: "操作步骤",
        steps: ["切换到“小组汇总日报”。", "使用日期范围查看历史记录。", "选择记录右侧的“打开”，可连续查看报告正文和下方的明日计划。", "选择“填写小组日报”并确认目标日期。", "直接编辑，或使用 AI 汇总本组成员已经保存的个人日报。", "检查成员遗漏和风险；明日计划需要人工维护，修改后再保存。"],
        screenshots: [{ alt: "小组汇总日报正文和明日计划", caption: "打开小组日报记录后，报告正文下方会显示明日计划。", src: "/help-center/screenshots/v1/tl/05-daily-team.png" }]
      }
    ]
  },
  {
    id: "daily-department-summary",
    module: "daily",
    title: "填写部门汇总日报",
    summary: "部门总监汇总部门进展、跨团队风险和重点事项。",
    route: "/reports/daily?tab=department",
    roles: ["director"],
    keywords: ["部门日报", "部门汇总", "部门总监", "填写部门日报"],
    sections: [
      {
        title: "操作步骤",
        steps: ["切换到“部门汇总日报”。", "使用日期范围查看历史记录。", "选择“展开”，可连续查看完整 Markdown 正文和下方的明日计划。", "选择“填写部门日报”并确认目标日期。", "直接编辑，或使用 AI 汇总所属小组已经保存的小组日报。", "检查缺失小组、跨团队风险和重点进展；明日计划需要人工维护，修改后再保存。"],
        screenshots: [{ alt: "部门汇总日报展开后的正文和明日计划", caption: "展开部门日报记录后，明日计划显示在完整正文下方。", src: "/help-center/screenshots/v1/dir/07-daily-department.png" }]
      }
    ]
  },
  {
    id: "weekly-overview",
    module: "weekly",
    title: "认识周报页面",
    summary: "按周管理个人报告；小组成员可查看同组周报，负责人还能维护对应范围的汇总周报。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["周报", "我的周报", "成员周报", "小组周报", "部门周报"],
    sections: [
      {
        title: "角色范围",
        bullets: [
          "工程师：个人周报记录和同组成员周报。",
          "产品经理：个人周报记录。",
          "团队负责人：个人、小组成员、小组汇总周报。",
          "部门总监：个人、部门成员、部门汇总周报。"
        ],
        screenshots: [
          { alt: "团队负责人个人周报", caption: "团队负责人可进入小组成员与小组汇总周报。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/06-weekly-personal.png" },
          { alt: "产品经理个人周报", caption: "产品经理使用个人周报记录与编辑流程。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/04-weekly-personal.png" },
          { alt: "部门总监个人周报", caption: "部门总监可进入部门成员与部门汇总周报。", roles: ["director"], src: "/help-center/screenshots/v1/dir/08-weekly-personal.png" }
        ]
      }
    ]
  },
  {
    id: "weekly-personal-records",
    module: "weekly",
    title: "查看和管理我的周报记录",
    summary: "在历史列表中快速预览、展开、复制和编辑对应周期的周报。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["我的周报", "历史记录", "展开", "复制全文", "编辑", "周期"],
    sections: [
      {
        title: "浏览与编辑",
        steps: ["查看每条记录的周起止日期和更新时间。", "折叠状态读取一行内容预览。", "展开后查看完整 Markdown，并可复制全文。", "选择“编辑”打开对应周期的周报管理弹窗。"]
      }
    ]
  },
  {
    id: "weekly-write",
    module: "weekly",
    title: "填写或补写周报",
    summary: "通过周报管理弹窗切换周次，填写当前周或历史周报告。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["填写周报", "补写周报", "周次", "周一", "保存"],
    sections: [
      {
        title: "操作步骤",
        steps: ["选择“填写周报”。", "在弹窗中确认或切换目标周。", "填写本周成果、风险和下周计划。", "保存后从历史记录继续查看或编辑。"]
      },
      {
        title: "周期口径",
        note: "页面按自然周管理记录，弹窗显示目标周起止日期。编辑历史记录时以该记录的周次为准。"
      }
    ]
  },
  {
    id: "weekly-ai-generate",
    module: "weekly",
    title: "用 AI 生成周报",
    summary: "根据目标周期和报告范围生成周报正文，再由用户检查和保存。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["AI 生成", "周报", "默认 Agent", "覆盖正文", "生成设置"],
    sections: [
      {
        title: "生成步骤",
        steps: ["打开目标周的周报管理弹窗。", "个人周报需要时先选择 session；不选择时系统按目标周冻结默认 Session 来源快照。", "选择“AI 生成”。", "生成完成后正文已自动保存，检查成果、风险和数据范围。", "人工修改后再选择“保存”。"]
      },
      {
        title: "重要说明",
        note: "AI 生成会替换并自动保存当前正文。小组周报汇总成员已保存的个人周报，部门周报汇总所属小组已保存的小组周报；两者不使用个人 session，也不会在下级报告缺失时回退读取成员原始 Session。"
      }
    ]
  },
  {
    id: "weekly-session-selection",
    module: "weekly",
    title: "选择 session 作为周报上下文",
    summary: "个人周报可自由选择相关 Session 切片，并将其固定为本次生成的权威 Session 来源。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["选择 session", "周报", "工作记录", "Token", "日期范围"],
    sections: [
      {
        title: "操作步骤",
        steps: ["打开个人周报管理弹窗。", "选择“选择 session”。", "默认不限制日期；需要时使用日期范围筛选候选切片。", "可选择目标周外或横跨多天的切片；列表默认每页 5 条，跨页选择不会丢失。", "勾选后直接关闭抽屉，无需确认按钮。", "返回编辑区执行 AI 生成；目标周仍是周报归属周期，但不会再次过滤已选切片。"],
        screenshots: [{ alt: "选择 Session 作为周报上下文", caption: "周报 Session 候选默认不限制日期，每页显示 5 条；目标周不会过滤已经勾选的切片。", src: "/help-center/screenshots/v1/dir/15-weekly-session-selection.png" }]
      }
    ]
  },
  {
    id: "weekly-member-reports",
    module: "weekly",
    title: "浏览成员周报",
    summary: "按周查看成员报告，在周会中快速展开重点内容并复制全文。",
    route: "/reports/weekly",
    roles: ["employee", "team_leader", "director"],
    keywords: ["成员周报", "小组成员", "部门成员", "周会", "复制"],
    sections: [
      {
        title: "浏览步骤",
        steps: ["切换到成员周报。", "选择目标周。", "在列表快速查看成员和内容预览。", "展开需要讨论的周报。", "需要外部使用时复制完整 Markdown。"],
        screenshots: [
          { alt: "小组成员周报", caption: "团队负责人看到本组成员周报。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/07-weekly-members.png" },
          { alt: "部门成员周报", caption: "部门总监看到全部部门成员周报。", roles: ["director"], src: "/help-center/screenshots/v1/dir/09-weekly-members.png" }
        ]
      },
      {
        title: "数据范围",
        roles: ["employee", "team_leader"],
        note: "普通成员和团队负责人只能看到本人所属小组成员的周报。"
      }
    ]
  },
  {
    id: "weekly-team-summary",
    module: "weekly",
    title: "填写小组汇总周报",
    summary: "团队负责人汇总小组一周成果、风险和下周计划。",
    route: "/reports/weekly",
    roles: ["team_leader"],
    keywords: ["小组周报", "小组汇总", "团队负责人", "填写小组周报"],
    sections: [
      {
        title: "操作步骤",
        steps: ["切换到“小组汇总周报”。", "选择“填写小组周报”并确认目标周。", "直接编辑或使用 AI 基于小组范围数据生成。", "核对成员成果、风险和下周计划后保存。"],
        screenshots: [{ alt: "小组汇总周报", caption: "小组汇总周报的历史记录与编辑入口。", src: "/help-center/screenshots/v1/tl/08-weekly-team.png" }]
      }
    ]
  },
  {
    id: "weekly-department-summary",
    module: "weekly",
    title: "填写部门汇总周报",
    summary: "部门总监汇总部门一周进展、跨团队风险和下周重点。",
    route: "/reports/weekly",
    roles: ["director"],
    keywords: ["部门周报", "部门汇总", "部门总监", "填写部门周报"],
    sections: [
      {
        title: "操作步骤",
        steps: ["切换到“部门汇总周报”。", "选择“填写部门周报”并确认目标周。", "直接编辑或使用 AI 基于部门范围数据生成。", "核对跨团队进展、风险和下周重点后保存。"],
        screenshots: [{ alt: "部门汇总周报", caption: "部门汇总周报的历史记录与编辑入口。", src: "/help-center/screenshots/v1/dir/10-weekly-department.png" }]
      }
    ]
  }
];

export function isBusinessRole(role: UserRole | undefined): role is BusinessRole {
  return role !== undefined && role !== "admin";
}

export function articleSupportsRole(article: HelpArticle, role: BusinessRole) {
  return !article.roles || article.roles.includes(role);
}

export function sectionSupportsRole(section: HelpSection, role: BusinessRole) {
  return !section.roles || section.roles.includes(role);
}

export function helpArticleSearchText(article: HelpArticle) {
  return [
    article.title,
    article.summary,
    ...article.keywords,
    ...article.sections.flatMap((section) => [
      section.title,
      ...(section.paragraphs ?? []),
      ...(section.bullets ?? []),
      ...(section.steps ?? []),
      ...(section.codeBlocks ?? []).flatMap((block) => [block.label, block.code]),
      ...(section.screenshotPlaceholders ?? []).flatMap((placeholder) => [placeholder.title, placeholder.description]),
      section.note ?? ""
    ])
  ]
    .join(" ")
    .toLocaleLowerCase();
}
