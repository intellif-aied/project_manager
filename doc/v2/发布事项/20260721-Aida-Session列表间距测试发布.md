# Aida Session 列表间距测试发布记录

> 发布日期：2026-07-21
> 环境：14.157 测试服
> 状态：已发布，待人工验收

## 发布范围

- Aida CLI：`0.1.16.4`
- 默认 TUI Session 选择列表：相邻 Session 之间增加一个空行，并按每条三行重新计算可视条数。
- 非 TTY 分页列表：相邻 Session 之间增加一个空行。
- JSON 输出、上传协议、API、数据库、Web、自动同步调度均未修改。

## 验证与回退

- 定向 Session Picker 和分页列表测试通过。
- `git diff --check` 通过。
- 三个平台发布文件 SHA256 校验通过。
- 下载验证目录：`/home/intellif/aida-release-verifications/20260721T093100Z-0.1.16.4`
- CLI 回退备份：`/home/intellif/aida-release-backups/20260721T093011Z-0.1.16.3`
- 测试服 API 健康检查正常，本次未重建或重启任何 Docker 服务。
