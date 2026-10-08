package utils

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

const (
	// RevisionAnnotation is the revision annotation of a deployment's replica sets which records its rollout sequence
	RevisionAnnotation = "deployment.kubernetes.io/revision"
	// TimedOutReason is added in a deployment when its newest replica set fails to show any progress
	// within the given deadline (progressDeadlineSeconds).
	TimedOutReason = "ProgressDeadlineExceeded"
)

// GetDeploymentCondition returns the condition with the provided type.
func GetDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// Revision returns the revision number of the input object.
func Revision(obj runtime.Object) (int64, error) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		return 0, err
	}
	v, ok := acc.GetAnnotations()[RevisionAnnotation]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(v, 10, 64)
}

type rsListFunc func(string, metav1.ListOptions) ([]*appsv1.ReplicaSet, error)

// rsListFromClient returns an rsListFunc that wraps the given client.
func rsListFromClient(ctx context.Context, c kubernetes.Interface) rsListFunc {
	return func(namespace string, options metav1.ListOptions) ([]*appsv1.ReplicaSet, error) {
		rsList, err := c.AppsV1().ReplicaSets(namespace).List(ctx, options)
		if err != nil {
			return nil, err
		}
		var ret []*appsv1.ReplicaSet
		for i := range rsList.Items {
			ret = append(ret, &rsList.Items[i])
		}
		return ret, err
	}
}

// FindNewReplicaSet returns the new RS this given deployment targets (the one with the same pod template).
func FindNewReplicaSet(deployment *appsv1.Deployment, rsList []*appsv1.ReplicaSet) (*appsv1.ReplicaSet, error) {
	newRSTemplate := GetNewReplicaSetTemplate(deployment)
	sort.Sort(ReplicaSetsByCreationTimestamp(rsList))
	for i := range rsList {
		if EqualIgnoreHash(rsList[i].Spec.Template, newRSTemplate) {
			// In rare cases, such as after cluster upgrades, Deployment may end up with
			// having more than one new ReplicaSets that have the same template as its template,
			// see https://github.com/kubernetes/kubernetes/issues/40415
			// We deterministically choose the oldest new ReplicaSet.
			return rsList[i], nil
		}
	}
	// new ReplicaSet does not exist.
	return nil, nil
}

func IsReplicaSetNew(deployment *appsv1.Deployment, rsMap map[string]*appsv1.ReplicaSet, rsName string) (bool, error) {
	rsList := []*appsv1.ReplicaSet{}
	for _, rs := range rsMap {
		rsList = append(rsList, rs)
	}

	newRs, err := FindNewReplicaSet(deployment, rsList)
	if err != nil {
		return false, err
	}

	return (newRs != nil) && (rsName == newRs.Name), nil
}

// GetNewReplicaSetTemplate returns the desired PodTemplateSpec for the new ReplicaSet corresponding to the given ReplicaSet.
// Callers of this helper need to set the DefaultDeploymentUniqueLabelKey k/v pair.
func GetNewReplicaSetTemplate(deployment *appsv1.Deployment) corev1.PodTemplateSpec {
	// newRS will have the same template as in deployment spec.
	return corev1.PodTemplateSpec{
		ObjectMeta: deployment.Spec.Template.ObjectMeta,
		Spec:       deployment.Spec.Template.Spec,
	}
}

// ListReplicaSets returns a slice of RSes the given deployment targets.
// Note that this does NOT attempt to reconcile ControllerRef (adopt/orphan),
// because only the controller itself should do that.
// However, it does filter out anything whose ControllerRef doesn't match.
func ListReplicaSets(deployment *appsv1.Deployment, getRSList rsListFunc) ([]*appsv1.ReplicaSet, error) {
	namespace := deployment.Namespace
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, err
	}
	options := metav1.ListOptions{LabelSelector: selector.String()}
	all, err := getRSList(namespace, options)
	if err != nil {
		return all, err
	}
	// Only include those whose ControllerRef matches the Deployment.
	owned := make([]*appsv1.ReplicaSet, 0, len(all))
	for _, rs := range all {
		controllerRef := GetControllerOf(rs)
		if controllerRef != nil && controllerRef.UID == deployment.UID {
			owned = append(owned, rs)
		}
	}
	return owned, nil
}

// EqualIgnoreHash returns true if two given podTemplateSpec are equal, ignoring the diff in value of Labels[pod-template-hash]
// We ignore pod-template-hash because the hash result would be different upon podTemplateSpec API changes
// (e.g. the addition of a new field will cause the hash code to change)
// Note that we assume input podTemplateSpecs contain non-empty labels
func EqualIgnoreHash(template1, template2 corev1.PodTemplateSpec) bool {
	// First, compare template.Labels (ignoring hash)
	labels1, labels2 := template1.Labels, template2.Labels
	if len(labels1) > len(labels2) {
		labels1, labels2 = labels2, labels1
	}
	// We make sure len(labels2) >= len(labels1)
	for k, v := range labels2 {
		if labels1[k] != v && k != appsv1.DefaultDeploymentUniqueLabelKey {
			return false
		}
	}
	// Then, compare the templates without comparing their labels
	template1.Labels, template2.Labels = nil, nil
	return apiequality.Semantic.DeepEqual(template1, template2)
}

// ReplicaSetsByCreationTimestamp sorts a list of ReplicaSet by creation timestamp, using their names as a tie breaker.
type ReplicaSetsByCreationTimestamp []*appsv1.ReplicaSet

func (o ReplicaSetsByCreationTimestamp) Len() int      { return len(o) }
func (o ReplicaSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o ReplicaSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// GetControllerOf returns the controllerRef if controllee has a controller,
// otherwise returns nil.
func GetControllerOf(controllee metav1.Object) *metav1.OwnerReference {
	ownerRefs := controllee.GetOwnerReferences()
	for i := range ownerRefs {
		owner := &ownerRefs[i]
		if owner.Controller != nil && *owner.Controller {
			return owner
		}
	}
	return nil
}

// debug 由 KUBEDOG_TRACKER_DEBUG 环境变量控制调试输出。
func debug() bool {
	return os.Getenv("KUBEDOG_TRACKER_DEBUG") == "1"
}

var _ = fmt.Sprintf // 保留 fmt 引用（debug 输出扩展时使用）
