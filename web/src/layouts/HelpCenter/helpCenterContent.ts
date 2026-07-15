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
        steps: ["执行 aida login。", "输入 AIDA 服务地址和刚复制的个人 Token。", "执行 aida status，核对当前登录账号。"],
        codeBlocks: [{ label: "登录并检查状态", code: "aida login\naida status" }]
      },
      {
        title: "4. 选择并上传 Session",
        steps: ["执行 aida sessions，检查项目、日期和摘要。", "执行 aida upload，交互选择一到两条安全的 Session。", "确认上传结果；首次使用不建议直接上传全部记录。"],
        codeBlocks: [
          { label: "查看本地 Session", code: "aida sessions" },
          { label: "交互选择并上传", code: "aida upload" }
        ],
        screenshotPlaceholders: [{ title: "aida sessions 与 upload", description: "等待你提供已检查敏感内容的真实上传过程截图后替换。" }]
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
          "可以直接选择“AI 生成”；不选择 Session 时，报告 Agent 会按照日报日期查找相对应的工作记录。",
          "如果只想总结指定工作，打开“选择 session”抽屉；抽屉里的开始日期和结束日期只用于筛选候选 Session。",
          "勾选需要的 Session 切片并关闭抽屉，再选择“AI 生成”。",
          "检查生成内容和明日计划，确认事实无误后保存。"
        ],
        screenshots: [
          { alt: "个人日报切换日期", caption: "弹窗顶部的日历选择日报归属日期，不负责筛选 Session。", src: "/help-center/screenshots/v1/quickstart/04-daily-date-select.png" },
          { alt: "日报 AI 生成入口", caption: "AI 生成和选择 Session 是并列操作；Session 选择不是必选步骤。", src: "/help-center/screenshots/v1/dir/13-daily-ai-generate.png" },
          { alt: "Session 日期范围筛选", caption: "打开 Session 抽屉后，开始日期和结束日期只筛选候选 Session，不会修改日报日期。", src: "/help-center/screenshots/v1/dir/12-daily-session-selection.png" }
        ]
      },
      {
        title: "完成检查",
        bullets: ["“我的日报记录”出现目标日期。", "展开后可以查看完整正文。", "仍可复制全文或再次编辑。"],
        screenshots: [{ alt: "日报保存结果", caption: "日报保存后会出现在个人日报记录列表中。", src: "/help-center/screenshots/v1/quickstart/03-daily-saved-result.png" }]
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
          "确认 aida upload 已经成功完成，而不是只执行了 aida sessions。",
          "确认日报弹窗顶部选择的是要生成的日报日期；这不是 Session 筛选条件。",
          "手动选择 Session 时，再在抽屉内使用开始日期和结束日期筛选候选记录。",
          "未手动选择 Session 时，重新执行 AI 生成，让报告 Agent 按日期查找记录。",
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
    keywords: ["找不到命令", "login 失败", "upload 失败", "status", "空列表"],
    sections: [
      {
        title: "快速排查",
        bullets: [
          "找不到 aida：重新执行安装，关闭终端后再打开，并用 aida --version 检查。",
          "身份不正确：重新执行 aida login，并用 aida status 核对账号。",
          "Session 列表为空：确认本机已使用 Claude Code 或 Codex 产生工作记录，必要时使用 --all 查看更早记录。",
          "上传失败：先减少选择数量重试，并检查网络与 AIDA 服务地址。"
        ]
      },
      {
        title: "继续阅读",
        paragraphs: ["更完整的安装、登录、上传参数和故障说明，请进入“AIDA 客户端”栏目。"]
      }
    ]
  },
  {
    id: "client-quick-start",
    module: "client",
    title: "第一次上传 Session",
    summary: "从安装、登录到上传，用四步把本地 Claude Code 或 Codex 工作记录同步到 AIDA。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["AIDA", "客户端", "CLI", "Session", "上传", "安装", "Claude Code", "Codex"],
    sections: [
      {
        title: "开始前确认",
        bullets: [
          "本机已经使用 Claude Code 或 Codex 完成过工作，客户端会扫描对应的本地 Session。",
          "你可以访问 AIDA，并可从右上角个人菜单选择“复制 Token”取得自己的访问令牌。",
          "令牌只用于本人设备登录，不要粘贴到群聊、需求描述或截图中。"
        ]
      },
      {
        title: "四步完成上传",
        steps: [
          "按操作系统安装 AIDA 客户端，安装后重新打开终端。",
          "执行 aida login，输入 AIDA 服务地址和个人访问令牌。",
          "执行 aida sessions，确认待上传记录的项目、时间和摘要。",
          "执行 aida upload 进入交互选择；确认后再上传需要的 Session。"
        ],
        codeBlocks: [
          { label: "检查本地 Session", code: "aida sessions" },
          { label: "交互选择并上传", code: "aida upload" }
        ],
        note: "首次使用建议交互选择，不建议直接使用 --all；这样可以先排除无关项目或包含敏感上下文的 Session。"
      },
      {
        title: "上传成功后",
        paragraphs: [
          "前往 Session 页面确认记录已出现。日报和周报在选择 Session 时，也会读取这些已上传记录。重复上传同一条 Session 时，客户端会跳过或更新已有记录，不会生成两条完全相同的数据。"
        ]
      }
    ]
  },
  {
    id: "client-install",
    module: "client",
    title: "安装或更新 AIDA 客户端",
    summary: "根据 Windows、macOS 或 Linux 选择安装命令；重复执行同一命令即可更新。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["安装", "更新", "Windows", "PowerShell", "macOS", "Linux", "找不到命令"],
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
    module: "client",
    title: "登录客户端并检查状态",
    summary: "使用个人令牌绑定 AIDA 服务，并确认当前终端登录身份正确。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["登录", "状态", "token", "令牌", "server", "身份"],
    sections: [
      {
        title: "登录",
        paragraphs: ["在 AIDA 右上角打开个人菜单，选择“复制 Token”；再把 <AIDA 服务地址> 和 <个人令牌> 替换为真实值。"],
        codeBlocks: [
          {
            label: "登录命令",
            code: "aida login --server <AIDA 服务地址>/api/v1 --token <个人令牌>"
          }
        ],
        note: "命令历史可能保留参数。更重视安全时，只执行 aida login --server <地址>/api/v1，再按提示输入令牌。"
      },
      {
        title: "核对身份",
        codeBlocks: [{ label: "查看登录状态", code: "aida status" }],
        bullets: [
          "Server 应指向当前使用的 AIDA 环境。",
          "User 应是你自己的姓名和角色。",
          "如果身份不符，不要继续上传，重新登录正确账号。"
        ]
      }
    ]
  },
  {
    id: "client-upload-troubleshooting",
    module: "client",
    title: "选择、上传与问题排查",
    summary: "掌握按项目筛选、批量上传和常见失败的处理方式。",
    route: "/sessions",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["upload", "sessions", "project", "all", "失败", "超时", "空列表"],
    sections: [
      {
        title: "常用命令",
        codeBlocks: [
          { label: "查看最近 Session", code: "aida sessions" },
          { label: "按项目筛选", code: "aida sessions --project <项目目录关键词>" },
          { label: "上传指定序号", code: "aida upload 1 3 5" },
          { label: "上传全部最近 Session", code: "aida upload --all" }
        ]
      },
      {
        title: "列表为空",
        bullets: [
          "确认本机确实运行过 Claude Code 或 Codex，并产生了 Session。",
          "不带 --all 时默认只显示最近活动记录；需要查更早记录可执行 aida sessions --all。",
          "如果使用项目筛选，先去掉 --project 排除关键词不匹配。"
        ]
      },
      {
        title: "上传失败或超时",
        steps: [
          "先执行 aida status，确认服务地址和身份。",
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
          "创建需求并写清目标、描述、负责人、参与团队、截止时间和验收标准。",
          "在需求详情中拆分任务，为任务设置负责人、截止时间和上游依赖。",
          "任务负责人持续更新任务状态与进度。",
          "通过风险和动态跟踪逾期、阻塞及关键变更。",
          "任务完成后对照验收标准确认需求是否达到完成条件。"
        ],
        screenshots: [
          { alt: "工程师需求看板", caption: "工程师仅处理与本人相关的需求和任务，不显示新建需求入口。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/02-requirements-board.png" },
          { alt: "团队负责人需求看板", caption: "团队负责人可创建和管理团队范围需求。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/02-requirements-board.png" },
          { alt: "产品经理需求看板", caption: "产品经理可管理跨团队需求、任务和验收。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/02-requirements-board.png" },
          { alt: "部门总监需求看板", caption: "部门总监可从部门范围跟踪需求阶段和风险。", roles: ["director"], src: "/help-center/screenshots/v1/dir/02-requirements-board.png" }
        ]
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
        bullets: ["阶段看板适合按状态推进。", "需求列表适合搜索、筛选和批量浏览。"]
      }
    ]
  },
  {
    id: "requirements-create",
    module: "requirements",
    title: "创建一条可执行的需求",
    summary: "把背景、负责人、参与团队、截止时间和验收标准一次写清。",
    route: "/requirements/create",
    roles: MANAGEMENT_ROLES,
    keywords: ["新建需求", "标题", "描述", "负责人", "团队", "截止", "验收标准"],
    sections: [
      {
        title: "创建步骤",
        steps: [
          "在需求看板选择“新建需求”。",
          "填写标题和需求描述，描述应说明背景、目标与范围。",
          "选择一个或多个负责人及参与团队。",
          "设置截止日期、优先级和可验证的验收标准。",
          "保存后进入需求详情继续拆分任务。"
        ]
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
          { alt: "需求详情抽屉", caption: "需求概览与任务、验收、动态集中在同一抽屉内。", roles: ["director"], src: "/help-center/screenshots/v1/dir/03-requirement-detail.png" }
        ]
      },
      {
        title: "快捷操作",
        bullets: ["有权限时可直接编辑截止时间、负责人和参与团队。", "“关联 session”位于标签栏右侧，用于管理需求级工作记录。"]
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
    keywords: ["拆分任务", "添加任务", "任务负责人", "进度", "状态", "完成"],
    sections: [
      {
        title: "拆分原则",
        bullets: ["每个任务只表达一个可交付结果。", "明确负责人、截止时间和完成标准。", "存在先后顺序时设置上游依赖。"]
      },
      {
        title: "推进步骤",
        steps: [
          "在需求详情的“任务”页签选择任务或添加任务。",
          "在任务详情中确认负责人、截止日期、进度和依赖。",
          "实际开始后更新状态和进度。",
          "遇到阻塞时先查看上游依赖和风险说明。",
          "完成后确认进度、状态和完成时间保持一致。"
        ]
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
        steps: ["创建或编辑任务时打开上游依赖选择器。", "搜索并选择必须先完成的任务。", "保存后在任务详情中核对依赖关系。"]
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
    keywords: ["验收", "AC", "完成标准", "验收标准"],
    sections: [
      {
        title: "写法建议",
        bullets: ["每条标准只描述一个可检查结果。", "避免“体验良好”“尽量优化”等不可验证表达。", "需求范围变化时同步更新验收标准，并通过动态确认变更。"]
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
        steps: ["打开需求详情。", "在标签栏右侧选择“关联 session”。", "按时间筛选并勾选对应工作记录。", "确认后在需求上下文中保留关联。"]
      },
      {
        title: "动态记录",
        bullets: ["动态按时间显示需求和任务的重要变更。", "动态用于追溯，不替代当前字段值。", "查看负责人、状态或关联记录变化时，可用动态确认是谁在何时操作。"]
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
    summary: "个人记录对所有角色开放；成员和汇总视图按管理范围出现。",
    route: "/reports/daily?tab=personal",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["日报", "我的日报", "成员日报", "小组日报", "部门日报"],
    sections: [
      {
        title: "所有角色",
        bullets: ["查看、筛选、展开和编辑个人日报记录。", "选择任意日期补写日报。", "使用 AI 生成个人日报，并可选择 session。"],
        screenshots: [
          { alt: "工程师个人日报", caption: "工程师只显示个人日报记录。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/03-daily-personal.png" },
          { alt: "团队负责人个人日报", caption: "团队负责人可在个人、小组成员和小组汇总之间切换。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/03-daily-personal.png" },
          { alt: "产品经理个人日报", caption: "产品经理使用个人日报记录与编辑流程。", roles: ["pm"], src: "/help-center/screenshots/v1/pm/03-daily-personal.png" },
          { alt: "部门总监个人日报", caption: "部门总监可在个人、部门成员和部门汇总之间切换。", roles: ["director"], src: "/help-center/screenshots/v1/dir/04-daily-personal.png" }
        ]
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
    keywords: ["个人日报", "日期筛选", "展开", "收起", "复制全文", "编辑"],
    sections: [
      {
        title: "浏览记录",
        steps: ["使用开始日期和结束日期筛选历史范围。", "在折叠状态快速阅读一行内容预览。", "选择“展开”加载并查看完整 Markdown 内容。", "展开后可复制全文或再次收起。"]
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
        steps: ["打开日报管理弹窗，通过日历图标确认或切换目标日期。", "个人日报可直接选择“AI 生成”，也可先选择 session 缩小上下文范围。", "生成完成后检查事实、结构和遗漏内容。", "确认无误后保存。"],
        screenshots: [{ alt: "日报 AI 生成入口", caption: "AI 生成与 Session 选择是并列操作；不选择 Session 也可以直接生成。", src: "/help-center/screenshots/v1/dir/13-daily-ai-generate.png" }]
      },
      {
        title: "AI 如何取得上下文",
        bullets: [
          "未选择 session：系统把目标日期交给报告 Agent，由 Agent 按日期查找和选择相对应的已上传 Session。",
          "已选择 session：AI 只使用当前勾选的 Session 切片作为个人日报上下文。",
          "Session 选择是可选的范围控制，不是生成日报的前置条件。",
          "小组或部门汇总日报不提供个人 Session 选择，AI 直接按目标日期和对应组织范围生成。"
        ]
      },
      {
        title: "重要说明",
        note: "AI 生成会替换当前编辑区正文；已有未保存内容时先确认是否继续。生成能力还依赖已配置的默认报告 Agent。"
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
        steps: ["在个人日报弹窗选择“选择 session”。", "按日期范围筛选记录。", "浏览摘要与 Token，用复选框选择相关记录。", "已选数量会立即更新，翻页不会丢失已选记录。", "关闭抽屉后选择“AI 生成”；无需额外确认按钮。"],
        screenshots: [{ alt: "选择 Session 作为日报上下文", caption: "按日期筛选后勾选需要的 Session；顶部会显示已选数量。截图只保留经过安全检查的测试记录。", src: "/help-center/screenshots/v1/dir/12-daily-session-selection.png" }]
      },
      {
        title: "什么时候需要选择",
        bullets: ["需要精确控制本次日报只总结哪些工作时选择 Session。", "不需要限定范围时可以不选，AI 会按日报日期自行查找相对应的 Session。", "选择后可使用清空按钮恢复“默认按日报日期取数”。"]
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
    summary: "在晨会或管理检查中按日期快速阅读成员日报，无需逐条打开详情页。",
    route: "/reports/daily?tab=member",
    roles: ["team_leader", "director"],
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
        roles: ["team_leader"],
        note: "团队负责人看到本人所属小组成员的日报。"
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
        steps: ["切换到“小组汇总日报”。", "使用日期范围查看历史记录。", "选择“填写小组日报”并确认目标日期。", "直接编辑或使用 AI 基于小组范围数据生成。", "检查成员遗漏、风险和计划后保存。"],
        screenshots: [{ alt: "小组汇总日报", caption: "小组汇总日报的历史记录和编辑入口。", src: "/help-center/screenshots/v1/tl/05-daily-team.png" }]
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
        steps: ["切换到“部门汇总日报”。", "使用日期范围查看历史记录。", "选择“填写部门日报”并确认目标日期。", "直接编辑或使用 AI 基于部门范围数据生成。", "检查跨团队风险、重点进展和后续计划后保存。"],
        screenshots: [{ alt: "部门汇总日报", caption: "部门汇总日报的历史记录和编辑入口。", src: "/help-center/screenshots/v1/dir/07-daily-department.png" }]
      }
    ]
  },
  {
    id: "weekly-overview",
    module: "weekly",
    title: "认识周报页面",
    summary: "按周管理个人报告；团队负责人和总监还能管理对应范围的成员与汇总周报。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["周报", "我的周报", "成员周报", "小组周报", "部门周报"],
    sections: [
      {
        title: "角色范围",
        bullets: [
          "工程师和产品经理：个人周报记录。",
          "团队负责人：个人、小组成员、小组汇总周报。",
          "部门总监：个人、部门成员、部门汇总周报。"
        ],
        screenshots: [
          { alt: "工程师个人周报", caption: "工程师只显示个人周报记录。", roles: ["employee"], src: "/help-center/screenshots/v1/emp/04-weekly-personal.png" },
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
        steps: ["打开目标周的周报管理弹窗。", "个人周报需要时先选择 session。", "选择“AI 生成”。", "检查成果、风险、数据范围和下周计划。", "确认后保存。"]
      },
      {
        title: "重要说明",
        note: "AI 生成会替换当前正文。小组和部门周报按组织范围聚合，不使用个人 session。"
      }
    ]
  },
  {
    id: "weekly-session-selection",
    module: "weekly",
    title: "选择 session 作为周报上下文",
    summary: "个人周报可从目标周内选择相关工作记录，让 AI 只汇总有效上下文。",
    route: "/reports/weekly",
    roles: ALL_BUSINESS_ROLES,
    keywords: ["选择 session", "周报", "工作记录", "Token", "日期范围"],
    sections: [
      {
        title: "操作步骤",
        steps: ["打开个人周报管理弹窗。", "选择“选择 session”。", "按日期范围浏览目标周工作记录。", "勾选相关记录并确认。", "返回编辑区执行 AI 生成。"]
      }
    ]
  },
  {
    id: "weekly-member-reports",
    module: "weekly",
    title: "浏览成员周报",
    summary: "按周查看成员报告，在周会中快速展开重点内容并复制全文。",
    route: "/reports/weekly",
    roles: ["team_leader", "director"],
    keywords: ["成员周报", "小组成员", "部门成员", "周会", "复制"],
    sections: [
      {
        title: "浏览步骤",
        steps: ["切换到成员周报。", "选择目标周。", "在列表快速查看成员和内容预览。", "展开需要讨论的周报。", "需要外部使用时复制完整 Markdown。"],
        screenshots: [
          { alt: "小组成员周报", caption: "团队负责人看到本组成员周报。", roles: ["team_leader"], src: "/help-center/screenshots/v1/tl/07-weekly-members.png" },
          { alt: "部门成员周报", caption: "部门总监看到全部部门成员周报。", roles: ["director"], src: "/help-center/screenshots/v1/dir/09-weekly-members.png" }
        ]
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
