package multitrack

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	dur "k8s.io/apimachinery/pkg/util/duration"

	"github.com/kubeop/kubedog/pkg/tracker/pod"
	"github.com/kubeop/kubedog/pkg/trackers/rollout/multitrack/generic"
)

// logger 返回本次 multitrack 使用的日志接收器。
// 通过 MultitrackOptions.Logger 注入（如写入 aiops 发布日志系统）；
// 未注入时回退到 JSON Lines 写 stdout 的默认接收器。
func (mt *multitracker) logger() LogSink {
	if mt.opts.Logger != nil {
		return mt.opts.Logger
	}
	return defaultLogSink
}

// defaultLogSink 进程级默认接收器，懒初始化，可被 SetDefaultLogSink 替换。
var defaultLogSink LogSink = NewDefaultLogSink()

// SetDefaultLogSink 设置进程级默认日志接收器（未注入 MultitrackOptions.Logger 时生效）。
func SetDefaultLogSink(sink LogSink) {
	if sink != nil {
		defaultLogSink = sink
	}
}

// emit 构造并发出一个事件（内部统一入口，保证字段一致）。
// namespace 优先取 data["namespace"]；缺失时回查 resourceNamespaces 注册表
// （Start 时按 resource 前缀注册，覆盖 kind/name 与嵌套 kind/name/po/xxx 两种形态）。
func (mt *multitracker) emit(t EventType, level Level, resource, message string, data map[string]any) {
	e := &Event{
		Time:     time.Now(),
		Type:     t,
		Level:    level,
		Resource: resource,
		Message:  strings.TrimRight(message, "\n"),
		Data:     data,
	}
	if ns, ok := data["namespace"].(string); ok && ns != "" {
		e.Namespace = ns
	} else if ns := mt.lookupNamespace(resource); ns != "" {
		e.Namespace = ns
	}
	mt.logger().WriteEvent(e)
}

// lookupNamespace 依据 resource 标识查 namespace：
// 支持 "deploy/name"、"deploy/name/po/xxx" 等，取前两段作为 key。
func (mt *multitracker) lookupNamespace(resource string) string {
	if len(mt.resourceNamespaces) == 0 || resource == "" {
		return ""
	}
	parts := strings.SplitN(resource, "/", 3)
	if len(parts) < 2 {
		return ""
	}
	key := parts[0] + "/" + parts[1]
	return mt.resourceNamespaces[key]
}

// emitStatus 发出资源状态快照事件。
func (mt *multitracker) emitStatus(resource string, data map[string]any) {
	mt.emit(EventResourceStatus, LevelInfo, resource, formatStatusMessage(resource, data), data)
}

func formatStatusMessage(resource string, data map[string]any) string {
	var parts []string
	for _, key := range []string{"status", "replicas", "ready", "uptodate", "available", "active", "succeeded", "failed", "duration", "weight", "condition"} {
		if v, ok := data[key]; ok && fmt.Sprint(v) != "" && fmt.Sprint(v) != "-" {
			parts = append(parts, fmt.Sprintf("%s=%v", key, v))
		}
	}
	if len(parts) == 0 {
		return resource
	}
	return fmt.Sprintf("%s (%s)", resource, strings.Join(parts, " "))
}

// emitLog 发出容器日志事件（多行合并为单个事件，行序保留在 data.lines）。
func (mt *multitracker) emitLog(resource, podName, containerName string, lines []string) {
	if len(lines) == 0 {
		return
	}
	msg := strings.Join(lines, "\n")
	mt.emit(EventResourceLog, LevelInfo, resource, msg, map[string]any{
		"pod":       podName,
		"container": containerName,
		"lines":     lines,
	})
}

func (mt *multitracker) displayResourceLogChunk(resourceKind string, spec MultitrackSpec, header string, chunk *pod.ContainerLogChunk) {
	if spec.SkipLogs {
		return
	}

	for _, containerName := range spec.SkipLogsForContainers {
		if containerName == chunk.ContainerName {
			return
		}
	}

	showLogs := len(spec.ShowLogsOnlyForContainers) == 0
	for _, containerName := range spec.ShowLogsOnlyForContainers {
		if containerName == chunk.ContainerName {
			showLogs = true
		}
	}

	if !showLogs {
		return
	}

	var logRegexp *regexp.Regexp
	if r := spec.LogRegexByContainerName[chunk.ContainerName]; r != nil {
		logRegexp = r
	} else if spec.LogRegex != nil {
		logRegexp = spec.LogRegex
	}

	showLines := []string{}

	if logRegexp != nil {
		for _, logLine := range chunk.LogLines {
			message := logRegexp.FindString(logLine.Message)
			if message != "" {
				showLines = append(showLines, logLine.Message)
			}
		}
	} else {
		for _, logLine := range chunk.LogLines {
			showLines = append(showLines, logLine.Message)
		}
	}

	if len(showLines) > 0 {
		// header 形如 "po/mypod-abc container/app"，解析出 pod 与容器名
		podName, containerName := parsePodContainerHeader(header)
		mt.emitLog(fmt.Sprintf("%s/%s", resourceKind, spec.ResourceName), podName, containerName, showLines)
	}
}

// parsePodContainerHeader 解析 "po/xxx container/yyy" 形式的 header。
func parsePodContainerHeader(header string) (podName, containerName string) {
	fields := strings.Fields(header)
	for _, f := range fields {
		if strings.HasPrefix(f, "po/") {
			podName = strings.TrimPrefix(f, "po/")
		}
		if strings.HasPrefix(f, "container/") {
			containerName = strings.TrimPrefix(f, "container/")
		}
	}
	return podName, containerName
}

func (mt *multitracker) displayResourceTrackerMessageF(resourceKind, resourceName string, showServiceMessages bool, format string, a ...interface{}) {
	resource := fmt.Sprintf("%s/%s", resourceKind, resourceName)
	msg := fmt.Sprintf(format, a...)
	mt.serviceMessagesByResource[resource] = append(mt.serviceMessagesByResource[resource], msg)

	if showServiceMessages {
		mt.emit(EventResourceServiceMessage, LevelInfo, resource, msg, nil)
	}
}

func (mt *multitracker) displayResourceEventF(resourceKind, resourceName string, showServiceMessages bool, format string, a ...interface{}) {
	resource := fmt.Sprintf("%s/%s", resourceKind, resourceName)
	msg := fmt.Sprintf(fmt.Sprintf("event: %s", format), a...)
	mt.serviceMessagesByResource[resource] = append(mt.serviceMessagesByResource[resource], msg)

	if showServiceMessages {
		mt.emit(EventResourceEvent, LevelInfo, resource, msg, nil)
	}
}

func (mt *multitracker) displayResourceErrorF(resourceKind, resourceName, format string, a ...interface{}) {
	resource := fmt.Sprintf("%s/%s", resourceKind, resourceName)
	mt.emit(EventResourceError, LevelError, resource, fmt.Sprintf(format, a...), nil)
}

func (mt *multitracker) displayFailedTrackingResourcesServiceMessages() {
	var parts []string

	for name, state := range mt.TrackingDeployments {
		if state.Status != resourceFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages("deploy", name)...)
	}
	for name, state := range mt.TrackingStatefulSets {
		if state.Status != resourceFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages("sts", name)...)
	}
	for name, state := range mt.TrackingDaemonSets {
		if state.Status != resourceFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages("ds", name)...)
	}
	for name, state := range mt.TrackingJobs {
		if state.Status != resourceFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages("job", name)...)
	}
	for name, state := range mt.TrackingCanaries {
		if state.Status != resourceFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages("canary", name)...)
	}
	for _, res := range mt.GenericResources {
		if res.State.ResourceState() != generic.ResourceStateFailed {
			continue
		}
		parts = append(parts, mt.collectResourceServiceMessages(res.Spec.GroupVersionKindNamespaceString(), res.Spec.Name)...)
	}

	if len(parts) > 0 {
		mt.emit(EventTrackingSummary, LevelError, "", strings.Join(parts, "\n"), map[string]any{
			"failedResources": parts,
		})
	}
}

// collectResourceServiceMessages 收集某资源累积的服务消息（不输出）。
func (mt *multitracker) collectResourceServiceMessages(resourceKind, resourceName string) []string {
	return mt.serviceMessagesByResource[fmt.Sprintf("%s/%s", resourceKind, resourceName)]
}

func (mt *multitracker) displayMultitrackServiceMessageF(format string, a ...interface{}) {
	mt.emit(EventResourceServiceMessage, LevelInfo, "", fmt.Sprintf(format, a...), nil)
}

func (mt *multitracker) displayStatusProgress() error {
	mt.emitDeploymentsStatusProgress()
	mt.emitDaemonSetsStatusProgress()
	mt.emitStatefulSetsStatusProgress()
	mt.emitJobsProgress()
	mt.emitCanariesProgress()
	mt.emitGenericsStatusProgress()
	return nil
}

func (mt *multitracker) emitCanariesProgress() {
	resourcesNames := []string{}
	for name := range mt.CanariesSpecs {
		resourcesNames = append(resourcesNames, name)
	}
	sort.Strings(resourcesNames)

	for _, name := range resourcesNames {
		status := mt.CanariesStatuses[name]
		spec := mt.CanariesSpecs[name]
		resource := fmt.Sprintf("canary/%s", name)

		data := map[string]any{
			"namespace": spec.Namespace,
			"isReady":   status.IsSucceeded,
			"isFailed":  status.IsFailed,
			"weight":    status.CanaryWeight,
			"lastUpdate": status.LastTransitionTime,
		}

		if status.IsFailed {
			data["status"] = "Failed"
			data["error"] = status.FailedReason
			mt.emit(EventResourceStatus, LevelError, resource, fmt.Sprintf("%s failed: %s", resource, status.FailedReason), data)
		} else {
			data["status"] = status.CanaryStatus.Phase
			mt.emitStatus(resource, data)
		}
	}
}

func (mt *multitracker) emitJobsProgress() {
	resourcesNames := []string{}
	for name := range mt.JobsSpecs {
		resourcesNames = append(resourcesNames, name)
	}
	sort.Strings(resourcesNames)

	for _, name := range resourcesNames {
		prevStatus := mt.PrevJobsStatuses[name]
		status := mt.JobsStatuses[name]
		spec := mt.JobsSpecs[name]
		resource := fmt.Sprintf("job/%s", name)

		if stillSucceeded := prevStatus.IsSucceeded && status.IsSucceeded; stillSucceeded {
			continue
		}

		data := mt.buildControllerStatusData("job", spec, status.IsSucceeded, status.IsFailed, status.FailedReason)

		data["active"] = status.Active
		data["succeeded"] = "-"
		if status.SucceededIndicator != nil {
			data["succeeded"] = plainIndicatorValue(status.SucceededIndicator.Value)
		}
		data["failed"] = status.Failed

		switch {
		case status.JobStatus.StartTime == nil:
		case status.JobStatus.CompletionTime == nil:
			data["duration"] = dur.HumanDuration(time.Since(status.JobStatus.StartTime.Time))
		default:
			data["duration"] = dur.HumanDuration(status.JobStatus.CompletionTime.Sub(status.JobStatus.StartTime.Time))
		}

		level := statusLevel(status.IsFailed, status.IsSucceeded)
		mt.emit(EventResourceStatus, level, resource, formatStatusMessage(resource, data), data)

		mt.emitChildPodsProgress(resource, prevStatus.Pods, status.Pods, status.WaitingForMessages)

		mt.PrevJobsStatuses[name] = status
	}
}

// buildControllerStatusData 组装控制器类资源（deploy/sts/ds/job）公共状态字段。
func (mt *multitracker) buildControllerStatusData(kind string, spec MultitrackSpec, isReady, isFailed bool, failedReason string) map[string]any {
	data := map[string]any{
		"namespace": spec.Namespace,
		"isReady":   isReady,
		"isFailed":  isFailed,
	}
	if isFailed && failedReason != "" {
		data["error"] = failedReason
	}
	return data
}

func statusLevel(isFailed, isReady bool) Level {
	switch {
	case isFailed:
		return LevelError
	case isReady:
		return LevelInfo
	default:
		return LevelInfo
	}
}

func plainIndicatorValue(v any) string {
	return fmt.Sprint(v)
}

func (mt *multitracker) emitStatefulSetsStatusProgress() {
	resourcesNames := []string{}
	for name := range mt.StatefulSetsSpecs {
		resourcesNames = append(resourcesNames, name)
	}
	sort.Strings(resourcesNames)

	for _, name := range resourcesNames {
		prevStatus := mt.PrevStatefulSetsStatuses[name]
		status := mt.StatefulSetsStatuses[name]
		spec := mt.StatefulSetsSpecs[name]
		resource := fmt.Sprintf("sts/%s", name)

		if stillDeployed := prevStatus.IsReady && status.IsReady; stillDeployed {
			continue
		}

		data := mt.buildControllerStatusData("sts", spec, status.IsReady, status.IsFailed, status.FailedReason)

		data["replicas"] = "-"
		if status.ReplicasIndicator != nil {
			data["replicas"] = fmt.Sprintf("%d/%d", status.ReplicasIndicator.Value, status.ReplicasIndicator.TargetValue)
		}
		data["ready"] = "-"
		if status.ReadyIndicator != nil {
			data["ready"] = fmt.Sprintf("%d/%d", status.ReadyIndicator.Value, status.ReadyIndicator.TargetValue)
		}
		data["uptodate"] = "-"
		if status.UpToDateIndicator != nil {
			data["uptodate"] = fmt.Sprintf("%d/%d", status.UpToDateIndicator.Value, status.UpToDateIndicator.TargetValue)
		}
		for _, w := range status.WarningMessages {
			data["warning"] = w
		}

		mt.emit(EventResourceStatus, statusLevel(status.IsFailed, status.IsReady), resource, formatStatusMessage(resource, data), data)

		mt.emitChildPodsProgress(resource, prevStatus.Pods, status.Pods, status.WaitingForMessages)

		mt.PrevStatefulSetsStatuses[name] = status
	}
}

func (mt *multitracker) emitDaemonSetsStatusProgress() {
	resourcesNames := []string{}
	for name := range mt.DaemonSetsSpecs {
		resourcesNames = append(resourcesNames, name)
	}
	sort.Strings(resourcesNames)

	for _, name := range resourcesNames {
		prevStatus := mt.PrevDaemonSetsStatuses[name]
		status := mt.DaemonSetsStatuses[name]
		spec := mt.DaemonSetsSpecs[name]
		resource := fmt.Sprintf("ds/%s", name)

		if stillDeployed := prevStatus.IsReady && status.IsReady; stillDeployed {
			continue
		}

		data := mt.buildControllerStatusData("ds", spec, status.IsReady, status.IsFailed, status.FailedReason)

		data["replicas"] = "-"
		if status.ReplicasIndicator != nil {
			data["replicas"] = fmt.Sprintf("%d/%d", status.ReplicasIndicator.Value, status.ReplicasIndicator.TargetValue)
		}
		data["available"] = "-"
		if status.AvailableIndicator != nil {
			data["available"] = fmt.Sprintf("%d/%d", status.AvailableIndicator.Value, status.AvailableIndicator.TargetValue)
		}
		data["uptodate"] = "-"
		if status.UpToDateIndicator != nil {
			data["uptodate"] = fmt.Sprintf("%d/%d", status.UpToDateIndicator.Value, status.UpToDateIndicator.TargetValue)
		}

		mt.emit(EventResourceStatus, statusLevel(status.IsFailed, status.IsReady), resource, formatStatusMessage(resource, data), data)

		mt.emitChildPodsProgress(resource, prevStatus.Pods, status.Pods, status.WaitingForMessages)

		mt.PrevDaemonSetsStatuses[name] = status
	}
}

func (mt *multitracker) emitDeploymentsStatusProgress() {
	resourcesNames := []string{}
	for name := range mt.DeploymentsSpecs {
		resourcesNames = append(resourcesNames, name)
	}
	sort.Strings(resourcesNames)

	for _, name := range resourcesNames {
		prevStatus := mt.PrevDeploymentsStatuses[name]
		status := mt.DeploymentsStatuses[name]
		spec := mt.DeploymentsSpecs[name]
		resource := fmt.Sprintf("deploy/%s", name)

		if stillDeployed := prevStatus.IsReady && status.IsReady; stillDeployed {
			continue
		}

		data := mt.buildControllerStatusData("deploy", spec, status.IsReady, status.IsFailed, status.FailedReason)

		data["replicas"] = "-"
		if status.ReplicasIndicator != nil {
			data["replicas"] = fmt.Sprintf("%d/%d", status.ReplicasIndicator.Value, status.ReplicasIndicator.TargetValue)
		}
		data["available"] = "-"
		if status.AvailableIndicator != nil {
			data["available"] = fmt.Sprintf("%d/%d", status.AvailableIndicator.Value, status.AvailableIndicator.TargetValue)
		}
		data["uptodate"] = "-"
		if status.UpToDateIndicator != nil {
			data["uptodate"] = fmt.Sprintf("%d/%d", status.UpToDateIndicator.Value, status.UpToDateIndicator.TargetValue)
		}

		mt.emit(EventResourceStatus, statusLevel(status.IsFailed, status.IsReady), resource, formatStatusMessage(resource, data), data)

		mt.emitChildPodsProgress(resource, prevStatus.Pods, status.Pods, status.WaitingForMessages)

		mt.PrevDeploymentsStatuses[name] = status
	}
}

func (mt *multitracker) emitGenericsStatusProgress() {
	for _, resource := range mt.GenericResources {
		res := fmt.Sprintf("%s", resource.Spec.ResourceID)

		data := map[string]any{
			"namespace": resource.Spec.Namespace,
			"isReady":   false,
			"isFailed":  false,
		}

		lastStatus := resource.State.LastStatus()
		if lastStatus == nil {
			data["condition"] = "-"
			mt.emitStatus(res, data)
			continue
		}

		lastPrintedStatus := resource.State.LastPrintedStatus()

		if stillReady := lastPrintedStatus != nil && lastPrintedStatus.IsReady() && lastStatus.IsReady(); stillReady {
			continue
		}

		data["isReady"] = lastStatus.IsReady()
		data["isFailed"] = lastStatus.IsFailed()

		var condition string
		if lastStatus.Indicator != nil {
			if lastStatus.IsFailed() && lastStatus.Indicator.FailedValue == "" {
				condition = "-"
			} else {
				condition = fmt.Sprintf("%s (target: %s)", lastStatus.Indicator.Value, lastStatus.Indicator.TargetValue)
			}
		} else {
			condition = "-"
		}

		if lastStatus.HumanConditionPath() != "" {
			data["condition"] = fmt.Sprintf("%s: %s", lastStatus.HumanConditionPath(), condition)
		} else {
			data["condition"] = condition
		}

		if lastStatus.IsFailed() && lastStatus.FailureReason() != "" {
			data["error"] = lastStatus.FailureReason()
			mt.emit(EventResourceStatus, LevelError, res, fmt.Sprintf("%s failed: %s", res, lastStatus.FailureReason()), data)
		} else {
			mt.emitStatus(res, data)
		}

		resource.State.SetLastPrintedStatus(lastStatus)
	}
}

// emitChildPodsProgress 为控制器资源发出子 Pod 状态事件。
func (mt *multitracker) emitChildPodsProgress(parentResource string, prevPods, pods map[string]pod.PodStatus, waitingForMessages []string) {
	podsNames := make([]string, 0, len(pods))
	for podName := range pods {
		podsNames = append(podsNames, podName)
	}
	sort.Strings(podsNames)

	for _, podName := range podsNames {
		podStatus := pods[podName]

		isReady := false
		if podStatus.StatusIndicator != nil {
			isReady = podStatus.StatusIndicator.IsReady()
		}

		data := map[string]any{
			"parent":   parentResource,
			"isReady":  isReady,
			"isFailed": podStatus.IsFailed,
			"ready":    fmt.Sprintf("%d/%d", podStatus.ReadyContainers, podStatus.TotalContainers),
			"restarts": podStatus.Restarts,
		}

		if podStatus.StatusIndicator != nil {
			data["status"] = podStatus.StatusIndicator.Value
		} else {
			data["status"] = "-"
		}

		podResource := fmt.Sprintf("%s/po/%s", parentResource, podName)

		if podStatus.IsFailed {
			data["error"] = podStatus.FailedReason
			mt.emit(EventResourceStatus, LevelError, podResource, fmt.Sprintf("%s failed: %s", podResource, podStatus.FailedReason), data)
		} else {
			mt.emitStatus(podResource, data)
		}
	}

	if len(waitingForMessages) > 0 {
		mt.emit(EventResourceStatus, LevelInfo, parentResource, fmt.Sprintf("%s waiting for: %s", parentResource, strings.Join(waitingForMessages, ", ")), map[string]any{
			"waitingFor": waitingForMessages,
		})
	}
}

// podContainerLogChunkHeader 保持原 header 语义（解析 pod/container 用）。
func podContainerLogChunkHeader(podName string, chunk *pod.ContainerLogChunk) string {
	return fmt.Sprintf("po/%s container/%s", podName, chunk.ContainerName)
}
