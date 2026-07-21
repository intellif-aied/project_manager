# AIDA-BUG-20260721-010：内网路由被失效 Token 误判为公网

## 1. 问题现象

在能够访问生产内网地址的机器上执行：

```text
aida status
```

输出仍选择公网地址：

```text
Server:  http://113.100.143.91:9180/api/v1 (public)
Public:  http://113.100.143.91:9180/api/v1
Status:  disconnected (unauthorized - token may be expired or invalid)
```

本机配置已开启自动路由，并配置内网地址：

```yaml
api_url: http://113.100.143.91:9180/api/v1
internal_api_url: http://192.168.14.182/api/v1
auto_route: true
```

## 2. 根因

`daemon/endpoint_router.go` 的 `resolveAPIEndpoint()` 默认选择 public，然后携带当前 Token 请求内网 `/auth/me`。

当前逻辑只有在以下条件全部满足时才选择 internal：

1. 内网请求成功；
2. HTTP 状态为 `200`；
3. 响应能够解析为非空用户身份。

Token 过期或无效时，内网 `/auth/me` 返回 `401`。客户端因此保留 public 路由，即使内网地址实际可达。随后公网 `/auth/me` 使用同一失效 Token，再次返回 unauthorized。

当前实现把“内网是否可达”和“当前 Token 是否有效”绑定成了同一个判断。

## 3. 影响

- `aida status` 会把可达的内网错误显示为 public；
- Token 失效场景无法准确反映机器的内外网路由状态；
- 用户容易误以为内网连接或自动路由配置异常；
- 重新登录取得有效 Token 后通常可以恢复 internal，但不能消除判定语义错误。

本问题不代表公网或内网服务不可用，也不影响服务端鉴权正确拒绝失效 Token。

## 4. 预期行为

路由可达性与身份鉴权应分别表达：

- 内网地址可达时，`Server` 应保持 internal；
- Token 失效时，`Status` 单独显示 unauthorized，并提示重新登录；
- 不应因为 `/auth/me` 返回 `401` 就把内网路由降级为 public；
- 网络错误、连接超时或明确不可达时才回退 public。

## 5. 当前状态

```text
状态：已确认，待修复
影响版本：Aida CLI 0.1.17
代码位置：daemon/endpoint_router.go
触发条件：auto_route=true、内网可达、当前 Token 失效或无效
临时处理：重新执行 aida login 获取有效 Token
```
