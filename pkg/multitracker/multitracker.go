// Package multitracker 是 kubedog 库的顶层入口，
// 封装 multitrack 多资源跟踪能力，面向将 kubedog 作为标准库引用的程序
// （如 aiops 平台的发布日志跟踪组件）。
//
// 最小用法：
//
//	err := multitracker.Run(ctx, multitracker.Specs{
//	    Deployments: []multitrack.MultitrackSpec{
//	        {ResourceName: "my-app", Namespace: "prod"},
//	    },
//	}, multitracker.Options{
//	    KubeClient: kubeClient,        // kubernetes.Interface，必填
//	    DynamicClient: dynamicClient,  // 使用 Generics 时必填
//	})
//
// 日志输出默认为 JSON Lines（stdout）；注入自定义 Sink 可对接发布日志系统：
//
//	multitracker.Options{
//	    ...
//	    Logger: multitracker.FuncLogSink(func(e *multitrack.Event) {
//	        publishLogToAIOPS(e) // 按需消费 resource_status / resource_log 等事件
//	    }),
//	}
package multitracker

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/kubeop/kubedog/pkg/trackers/rollout/multitrack"
)

// Specs 是待跟踪资源集合的别名，字段与 multitrack.MultitrackSpecs 一致：
// Deployments / StatefulSets / DaemonSets / Jobs / Canaries / Generics。
type Specs = multitrack.MultitrackSpecs

// Spec 单个跟踪资源规格别名（Deployment/StatefulSet/DaemonSet/Job/Canary 通用）。
type Spec = multitrack.MultitrackSpec

// Options 顶层跟踪选项。
type Options struct {
	// KubeClient Kubernetes clientset，必填。
	KubeClient kubernetes.Interface
	// DynamicClient dynamic client，跟踪 Generics（任意自定义资源）时必填。
	DynamicClient dynamic.Interface
	// DiscoveryClient discovery client（缓存），跟踪 Generics 时必填。
	DiscoveryClient discovery.CachedDiscoveryInterface
	// Mapper REST mapper，跟踪 Generics 时必填。
	Mapper meta.RESTMapper

	// Logger 结构化日志接收器；nil 时使用 JSON Lines 写 stdout。
	Logger multitrack.LogSink

	// Timeout 整个跟踪会话的超时（0 表示无限等待）。
	Timeout time.Duration
	// StatusProgressPeriod 状态快照输出周期（默认 5s；<=0 关闭周期输出）。
	StatusProgressPeriod time.Duration
	// ParentContext 取消/超时控制上下文。
	ParentContext context.Context
}

// Run 并发跟踪 specs 中的所有资源，直到全部就绪、失败或超时。
// 返回 nil 表示全部资源就绪；返回错误时错误信息包含失败资源清单。
func Run(ctx context.Context, specs Specs, opts Options) error {
	mtOpts := multitrack.MultitrackOptions{
		DynamicClient:        opts.DynamicClient,
		DiscoveryClient:      opts.DiscoveryClient,
		Mapper:               opts.Mapper,
		Logger:               opts.Logger,
		StatusProgressPeriod: opts.StatusProgressPeriod,
	}
	mtOpts.Timeout = opts.Timeout
	mtOpts.ParentContext = ctx
	if opts.ParentContext != nil {
		mtOpts.ParentContext = opts.ParentContext
	}

	return multitrack.Multitrack(opts.KubeClient, specs, mtOpts)
}
