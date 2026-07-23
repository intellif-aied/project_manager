# Git Worktree 独立功能开发流程

## 1. 目的

本流程用于在 `main` 正在进行测试、发布准备或其他开发时，隔离开发一个独立功能或缺陷修复，并在完成后安全合并回 `main`。

固定原则：

- `main` 工作区只用于集成、测试和发布准备，不直接承载独立功能开发。
- 一个独立任务对应一个分支和一个 worktree。
- worktree 只隔离工作目录，不隔离 Docker、端口、数据库、缓存和外部服务。
- 独立 worktree 默认只执行静态检查和自动化测试，不启动或复用当前共享运行环境。
- 未经用户明确指令，不推送、不合并、不发布、不执行生产操作。
- 数据库迁移只允许在独立临时数据库验证，不得直接应用到共享测试库或生产库。

## 2. 适用范围

以下情况必须使用独立 worktree：

- 用户要求开发独立功能或缺陷修复，且 `main` 正在进行其他开发、测试或发布准备；
- 任务预计会修改业务代码、测试或数据库迁移；
- 用户明确要求隔离开发。

以下情况可以直接在当前工作区处理：

- 只读分析或代码审查；
- 用户明确要求在当前分支完成的纯文档小改动；
- 用户明确指定其他工作方式。

## 3. 创建前检查

在主工作区执行：

```bash
cd /home/intellif/dev/project_manager
git status --short --branch
git branch --show-current
git log -5 --oneline
git worktree list
```

出现以下任一情况时停止创建并向用户说明：

- 仓库正在执行 merge、rebase 或 cherry-pick；
- `main` 存在与本任务预期修改文件重叠的未提交跟踪改动；
- 无法确认创建分支所基于的提交；
- 目标分支或 worktree 路径已存在且无法确认归属；
- 任务必须修改共享运行环境或共享数据库，但尚未确定隔离方式。

与任务无关的未跟踪文件保持原样，不删除、不移动、不纳入提交。若目标路径与其冲突，则更换 worktree 路径。

## 4. 命名与创建

分支命名固定为：

- 功能：`feat/<短名称>`
- 修复：`fix/<短名称>`

worktree 固定放在：

```text
/home/intellif/dev/project_manager-worktrees/<短名称>
```

创建前，Codex 必须向用户报告基准分支、基准提交、目标分支和目标路径。用户已经明确要求开始独立功能开发时，无需再次询问是否创建。

示例：

```bash
repo_root=/home/intellif/dev/project_manager
task_branch=fix/report-run-id-only
task_worktree=/home/intellif/dev/project_manager-worktrees/report-run-id-only

git -C "$repo_root" worktree add -b "$task_branch" "$task_worktree" main
git -C "$task_worktree" status --short --branch
git -C "$task_worktree" log -1 --oneline
```

不得在主工作区切换到任务分支。

## 5. 开发流程

进入新 worktree 后按以下顺序执行：

1. 完整阅读根 `AGENTS.md`、相关需求文档、开发方案和测试验收文档。
2. 检查真实代码路径，确认实现与文档一致；发现冲突时先停止开发并说明。
3. 只修改当前任务所需文件，不顺带重构无关代码。
4. 使用精确文件路径编辑和暂存，保留其他协作者的改动。
5. 先执行与改动直接相关的自动化测试，再执行仓库规定的必要检查。
6. 每个提交只包含当前任务内容。

不得使用 `git add .`、`git reset --hard`、`git checkout -- <file>` 或其他可能覆盖协作者工作的命令。

## 6. 运行环境隔离

独立 worktree 默认不启动 Docker Compose、不启动新的前端开发进程，也不连接共享数据库进行写入测试。

允许直接执行：

- 后端单元测试和不依赖共享服务的集成测试；
- 前端类型检查、Lint、单元测试和构建；
- `git diff --check` 等静态检查。

必须验证数据库迁移时，使用独立临时数据库，并满足：

- 独立容器名；
- 独立数据库名；
- 不复用现有宿主机端口；
- 测试完成后只清理本任务创建的临时资源。

必须启动完整运行环境时，需先获得用户明确同意，并为本任务单独设置 Compose project name、端口、环境变量、数据库和卷。不得复用现有 `project_manager` 或 `aida` 的容器名、端口、数据库和卷。

## 7. 提交前检查

在任务 worktree 中依次执行：

```bash
git status --short --branch
git diff --check
git diff --name-only
git add -- <本任务的明确文件列表>
git diff --cached --check
git diff --cached --name-only
git commit -m "<符合仓库约定的提交信息>"
git status --short --branch
```

提交后工作区必须干净。未经用户明确指令，不执行 `git push`。

## 8. 合并前同步

满足以下条件后才能准备合并：

- 功能与测试已经完成；
- 任务 worktree 没有未提交改动；
- `main` 当前工作已经提交完成；
- `main` 不在 merge、rebase 或 cherry-pick 中；
- 用户已经明确要求合并。

先在任务 worktree 合并最新 `main`：

```bash
git -C "$task_worktree" merge main
```

如有冲突，只解决与本任务有关的冲突；无法确认其他改动归属时停止并向用户说明。解决冲突后重新执行本任务全部必要测试并提交合并结果。

## 9. 合并到 main

确认主工作区干净且用户已要求合并后执行：

```bash
git -C "$repo_root" merge --no-ff "$task_branch"
git -C "$repo_root" diff --check HEAD^
git -C "$repo_root" status --short --branch
```

合并后在 `main` 执行与改动相关的自动化测试。不得因为合并而自动重启服务、执行数据库迁移、推送或发布；这些操作分别等待用户明确指令。

## 10. 清理 worktree

只有同时满足以下条件才允许清理：

- 任务分支已经合并到 `main`；
- `main` 上的合并后验证已通过；
- 任务 worktree 没有未提交改动；
- 用户已要求清理，或用户先前明确约定合并完成后立即清理。

执行：

```bash
git -C "$task_worktree" status --porcelain
git -C "$repo_root" branch --merged main
git -C "$repo_root" worktree remove "$task_worktree"
git -C "$repo_root" branch -d "$task_branch"
git -C "$repo_root" worktree list
```

不得使用 `worktree remove --force`、`git branch -D`，也不得直接删除 worktree 目录。任一检查不满足时停止清理。

清理 worktree 不会删除已经合并到 `main` 的提交。尚未合并的 worktree 不得清理。

## 11. 异常处理与回退

- 创建失败：不修改已有分支或 worktree，查明冲突后报告用户。
- 自动化测试失败：保留任务 worktree，在其中修复，不把失败提交合并到 `main`。
- 合并冲突：在任务 worktree 先同步并解决，不在存在其他开发改动的主工作区试错。
- 合并后发现问题：优先创建新的修复提交；需要撤销合并时，只能在用户明确要求后使用 `git revert -m 1 <merge提交>`。
- 禁止通过重置 `main`、强制删除分支或覆盖工作区进行回退。

## 12. Codex 汇报要求

创建 worktree 后必须报告：

- 基准分支和基准提交；
- 任务分支和 worktree 路径；
- 是否会启动独立运行环境；
- 当前主工作区是否存在未触碰的其他改动。

开发完成后必须报告：

- 修改文件和行为；
- 已执行测试及结果；
- 未执行的测试及原因；
- 提交、推送、合并、发布状态。

清理完成后必须报告：

- 已删除的 worktree 路径；
- 已删除的本地任务分支；
- 远程分支是否仍保留；
- `main` 的最终提交。

## 13. 固定流程摘要

```text
检查 main 和已有 worktree
  -> 从 main 当前提交创建任务分支和独立 worktree
  -> 在独立 worktree 开发并执行自动化测试
  -> 提交任务改动
  -> 等待 main 当前工作完成
  -> 在任务 worktree 合并最新 main 并重新测试
  -> 用户明确要求后合并到 main
  -> main 上执行合并后验证
  -> 用户授权或已有约定后删除 worktree 和本地任务分支
```
