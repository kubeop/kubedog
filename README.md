# kubedog

kubedog 是一个 **纯标准库（library-only）**：并发跟踪 Kubernetes 资源
（Deployment / StatefulSet / DaemonSet / Job / Canary / 任意自定义资源），
直到全部就绪、失败或超时，并以**结构化日志事件**输出全过程。



本仓库基于 [werf/kubedog](https://github.com/werf/kubedog) 的发布跟踪组件裁剪而来：
- 仅保留 multitracker 多资源跟踪能力；
- 无 CLI、无终端渲染依赖（ANSI 颜色 / 终端宽度自适应 / TTY 进度条全部移除）；
- 日志输出为结构化事件流（默认 JSON Lines），便于平台直接解析入库、按资源分组展示。



## 安装

```bash
go get github.com/kubeop/kubedog
```



## 快速上手

### 1. 初始化 Kubernetes 客户端

```go
import "github.com/kubeop/kubedog/pkg/kube"

err := kube.Init(kube.InitOptions{
    KubeConfigOptions: kube.KubeConfigOptions{
        ConfigPath: "/path/to/kubeconfig", // 或 ConfigDataBase64 / in-cluster 自动发现
    },
})
```



### 2. 跟踪资源直到就绪

```go
import "github.com/kubeop/kubedog/pkg/multitracker"

err := multitracker.Run(ctx, multitracker.Specs{
    Deployments: []multitrack.MultitrackSpec{
        {ResourceName: "web", Namespace: "prod"},
    },
    Jobs: []multitrack.MultitrackSpec{
        {ResourceName: "migrate", Namespace: "prod"},
    },
}, multitracker.Options{
    KubeClient:   kube.Kubernetes,
    DynamicClient: kube.DynamicClient, // 跟踪 Generics 时必填
    Timeout:      10 * time.Minute,
})
```

返回 `nil` 表示全部资源就绪；失败时错误信息包含失败资源清单与原因。



### 3. 接管日志输出（对接 aiops 发布日志）

默认输出 JSON Lines 到 stdout。注入 `LogSink` 即可将事件流转投到平台：

```go
import "github.com/kubeop/kubedog/pkg/trackers/rollout/multitrack"

opts := multitracker.Options{
    KubeClient: kube.Kubernetes,
    Logger: multitrack.FuncLogSink(func(e *multitrack.Event) {
        switch e.Type {
        case multitrack.EventResourceStatus: // 周期性状态快照 → 追加到资源时间线
        case multitrack.EventResourceLog:     // 容器日志 → 按 pod/container 分组展示
        case multitrack.EventResourceError:   // 跟踪失败 → 标红并终止发布
        }
    }),
}
```



## 事件模型

每条日志是一个 JSON 对象：

```json
{"time":"2026-10-08T10:15:30+08:00","type":"resource_status","level":"info",
 "resource":"deploy/web","namespace":"prod",
 "message":"deploy/web (replicas=3/3, available=3/3, uptodate=3/3)",
 "data":{"replicas":"3/3","available":"3/3","uptodate":"3/3","isReady":true}}
```

| type | 说明 | 典型 data 字段 |
|------|------|----------------|
| `resource_status` | 资源状态快照（周期/变化时） | `isReady` `isFailed` `replicas` `available` `uptodate` `status` `condition` `error` |
| `resource_log` | 容器日志块 | `pod` `container` `lines` |
| `resource_service_message` | 资源级服务消息（added / become READY 等） | — |
| `resource_event` | K8s Event 转发 | — |
| `resource_error` | 资源跟踪失败原因 | — |
| `tracking_summary` | 会话级汇总（失败资源服务消息清单） | `failedResources` |

- `level`：`info` / `warning` / `error` / `debug`；
- `resource`：`kind/name` 或嵌套 `deploy/web/po/web-abc` 形式，可直接作为分组键。



## 核心包

| 包 | 职责 |
|----|------|
| `pkg/multitracker` | 顶层入口：`Run(ctx, specs, opts)` |
| `pkg/trackers/rollout/multitrack` | multitracker 核心实现与事件模型（`Event` / `LogSink`） |
| `pkg/kube` | 客户端初始化（kubeconfig / in-cluster / base64） |
| `pkg/tracker/*` | 各资源类型 tracker（deploy/sts/ds/job/pod/generic/canary） |
| `pkg/informer` | 共享 dynamic informer 工厂 |



## 高级用法

### 跟踪任意自定义资源（Generics）

```go
specs.Generics = []*generic.Spec{{
    ResourceID: &resid.ResourceID{
        Name: "my-crd", Namespace: "prod",
        GroupVersionKind: schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "MyCRD"},
    },
    ShowServiceMessages: true,
}}
```

需要同时提供 `DynamicClient` / `DiscoveryClient` / `Mapper`。



### 失败策略

每个 spec 可配置 `FailMode`：
- `FailWholeDeployProcessImmediately`（默认）：失败计数超过 `AllowFailuresCount` 立即终止整个跟踪；
- `IgnoreAndContinueDeployProcess`：仅记录错误继续跟踪；
- `HopeUntilEndOfDeployProcess`：等待其他资源就绪后再开始计数。



### 状态输出周期

`Options.StatusProgressPeriod` 默认 5 秒；设为负值关闭周期快照（仅事件驱动输出）。



## 验证

```bash
go build ./...
go vet ./...
go test ./...
go test -tags ai_tests ./pkg/tracker/generic/ ./pkg/tracker/event/
```



## License

Apache License 2.0.
