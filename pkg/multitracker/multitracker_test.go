package multitracker_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/kubeop/kubedog/pkg/multitracker"
	"github.com/kubeop/kubedog/pkg/trackers/rollout/multitrack"
)

// TestRunEmptySpecs 空规格应直接成功返回（不触碰集群）。
func TestRunEmptySpecs(t *testing.T) {
	err := multitracker.Run(context.Background(), multitracker.Specs{}, multitracker.Options{})
	if err != nil {
		t.Fatalf("Run with empty specs should return nil, got: %v", err)
	}
}

// TestRunOptionsToMultitrackOptions 校验顶层 Options 正确透传到 multitrack 层
// （以 Multitrack 入口的行为为准：空 specs 时不访问 client，故可传 nil client）。
func TestRunOptionsPassthrough(t *testing.T) {
	var captured *multitrack.Event

	opts := multitracker.Options{
		KubeClient:            kubernetes.Interface(nil),
		Logger:                multitrack.FuncLogSink(func(e *multitrack.Event) { captured = e }),
		Timeout:               3 * time.Second,
		StatusProgressPeriod:   -1,
	}

	// 空 specs：Run 应立即返回且未发出事件
	if err := multitracker.Run(context.Background(), multitracker.Specs{}, opts); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured != nil {
		t.Fatalf("no events expected for empty specs, got: %+v", captured)
	}
}
