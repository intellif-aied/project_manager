# 第三轮真实链路与 Agent 验收

> 执行日期：2026-07-17  
> Digest 版本：`session-digest/v2.9.0`  
> 环境：14.159 冻结样本 -> 14.157 开发测试环境  
> 当前状态：隔离上传与服务端 Digest 构建通过，等待用户手动选择 Session

## 1. 上传隔离结论

本轮不调用 14.159 用户已安装的 Aida，不读取或修改共享 `~/.aida.yaml`、上传索引和常驻进程状态。

真实链路使用：

- 独立 worktree 源码构建的临时二进制 `/tmp/aida-stage3`；
- 独立 `HOME=/tmp/aida-stage3-home`；
- 隔离 Session 目录，仅放置 S01-S10 原文件的只读软链接；
- 显式注入 14.157 API、t01 Token，并关闭自动更新；
- 运行前通过 `/auth/me` 和 `aida status` 双重确认 uid 303 / t01；
- 测试二进制 SHA-256 为 `1c23a0903e5a7b743d3968bb2d2ac3528abef02b709200304536fc8b20c900e9`，与用户安装版 SHA-256 `a272cf5920243023be03e8351ba83ff6c4b476fee1ec844262f74f6709b5e2ee` 不同。

`aida upload --all` 仅发现冻结的 10 个样本，10/10 readiness 为 READY。由于 14.157 已保存相同 SHA 的完整原文，服务端按正常协议返回 `incremental=unchanged`、`chunks=0` 并复用原文；这验证了真实客户端扫描、身份、prepare 和 readiness 链路，没有直接写数据库，也没有伪造上传结果。

## 2. 14.157 Digest 构建结果

服务端已为 10 个 Session 的 11 个活动切片全部构建 `v2.9.0`，状态均为 `ready`。S10 跨日，因此对应两个独立切片。

| 样本 | Session ID | Slice key | Digest 字节 | Digest SHA-256 |
| --- | --- | --- | ---: | --- |
| S01 | `019f1d50-19f8-7253-a141-0c2ce417d6c0` | `83dc3dae-1297-457d-a6f5-af48661958a2` | 516,571 | `38e5adf99a0a024d10fed36bd33184895ceb98ffa9e09cb8f693850c6649fbe0` |
| S02 | `019f1d70-3d87-73f2-a849-03b45a15f5fe` | `b6c7a6ed-6bfe-404f-9109-49aa5179cbb8` | 485,727 | `88555595c2d8205d967b90162d4b8a42cde3bbd5f48b053897c0e76eb7ba5f1f` |
| S03 | `019f4bac-2026-7072-83f2-1a93ea32f2bd` | `fe943d51-db2a-46a9-a501-08d0b5e42836` | 204,465 | `6bbb6d6e37bbe6180982143af9279887d978413b30485cca2475c897dd064d13` |
| S04 | `019f68d0-21e2-7160-a7de-d319e1ad2622` | `df071136-3818-49d7-ac16-04180f6a989d` | 232,912 | `d3db99b0b0b4df1b1ad99b7de05ec67515a81ab1180b6d392037a8e0464a520e` |
| S05 | `019ecf3c-40c8-7f02-b09e-3c336f4363bb` | `6d549be2-a7a4-40d5-b48e-c82694dda358` | 42,014 | `458e76c69c40b14bf37dbe16afaf4a86b5eb6a1e6bd89c8509a12959f5f19aa3` |
| S06 | `019f6bc4-08b8-74a3-b1cb-72bf9b0c874f` | `c63a24e0-c6cb-4050-af4d-e2bbe4e16146` | 62,312 | `d1204fd6fdc0d25316ea7319ea2397f7727938920e1dfc4972f21169b5684662` |
| S07 | `019f44e7-45c6-7200-b5df-71daf81f9d33` | `b33e23f6-58f7-4f8c-beda-8c991d2f509e` | 956,551 | `87b55a063c41bc42d64447976d279d1fc8efc458cc94415d303881cff89c9452` |
| S08 | `019ece38-f7b7-7cf2-b209-d7eaf12e3c54` | `995213cb-3d16-4054-9905-77815cd99a14` | 217,870 | `1e256b7b301ff47aff258e4cb3ef8dc9a992414e28e749ecc640cee847df998c` |
| S09 | `019f4570-fd80-7ec1-ae1d-e1a469154d69` | `c2b85e61-87b3-4c24-b77c-c6103091955f` | 2,815 | `73fb175c2bdfbc100e412e4fd715d057b182b37d26077e685c29c696987c5090` |
| S10-A | `019f68ce-9a8a-7330-b1a6-6ac55fbe38f2` | `3f648a16-2d78-474b-9585-2ff0ad2336a4` | 22,404 | `4c9aaa749841dc0f58cc9f2f9dccc0434023bb7be6e435acd5f1b7972b25c437` |
| S10-B | `019f68ce-9a8a-7330-b1a6-6ac55fbe38f2` | `cdf09231-c5fa-4213-b414-af83c17b4728` | 71,135 | `00de363842d09910d016cd17fd95c83eea364fb0c53f0845663af100ded8927b` |

S01-S10 的服务端 Digest 均低于 1 MiB。`truncated=true` 表示确定性投影和噪音清理发生过，不表示报告事实被按 Top-K 删除。

## 3. 用户手动选择清单

自动化程序到此停止，不调用 `POST /report-source-selections`。请使用 t01 在 14.157 页面按下表手动选择并发起日报；同一报告日期仅选择对应样本，避免跨日期结果混入。

| 批次 | 报告日期 | 手动选择的样本 | Slice key |
| --- | --- | --- | --- |
| B01 | 2026-07-06 | S01 | `83dc3dae-1297-457d-a6f5-af48661958a2` |
| B02 | 2026-07-15 | S02 | `b6c7a6ed-6bfe-404f-9109-49aa5179cbb8` |
| B03 | 2026-07-11 | S03 | `fe943d51-db2a-46a9-a501-08d0b5e42836` |
| B04 | 2026-07-16 | S04 | `df071136-3818-49d7-ac16-04180f6a989d` |
| B05 | 2026-06-16 | S05 | `6d549be2-a7a4-40d5-b48e-c82694dda358` |
| B06 | 2026-07-17 | S06 | `c63a24e0-c6cb-4050-af4d-e2bbe4e16146` |
| B07 | 2026-07-10 | S07 | `b33e23f6-58f7-4f8c-beda-8c991d2f509e` |
| B08 | 2026-06-22 | S08 | `995213cb-3d16-4054-9905-77815cd99a14` |
| B09 | 2026-07-09 | S09 | `c2b85e61-87b3-4c24-b77c-c6103091955f` |
| B10 | 2026-07-16 | S10，两条都选 | `3f648a16-2d78-474b-9585-2ff0ad2336a4`、`cdf09231-c5fa-4213-b414-af83c17b4728` |

手动发起后记录每次返回的 selection ID、Agent run ID 或 Agent Session ID。后续校对只读取运行日志和持久化日报，不修改 Digest、Report Skill、Agent Prompt 或模型。

## 4. 当前验收结论

- 隔离上传：通过；未使用用户本机 Aida 客户端。
- readiness：10/10 通过。
- `v2.9.0` Digest：10 个 Session、11 个切片全部 ready。
- Layer A 离线质量：通过，详见第二轮文档。
- Layer B 真实 Agent 日报：等待用户手动选择，尚未判定。

本阶段尚未完成，不得据此发布生产。
