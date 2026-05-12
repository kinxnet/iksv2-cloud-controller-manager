package kinx

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// servicePatcher holds a baseline and an updated copy of a Service object so
// that a strategic-merge-patch can be computed and applied via the Kubernetes
// API.  The pattern mirrors cloud-provider-openstack's loadbalancer_service_patcher.go.
type servicePatcher struct {
	kclient kubernetes.Interface
	base    *corev1.Service
	updated *corev1.Service
}

// newServicePatcher creates a servicePatcher.  base is deep-copied so that
// subsequent mutations to updated (which points at the original object) are
// recorded correctly.
func newServicePatcher(kclient kubernetes.Interface, base *corev1.Service) servicePatcher {
	return servicePatcher{
		kclient: kclient,
		base:    base.DeepCopy(),
		updated: base,
	}
}

// patch computes a strategic merge patch between base and updated and, when
// the annotations differ, applies it against the Kubernetes API server.
// Errors are logged but never propagated — annotation recording is best-effort.
//
// client-go v0.18 Patch signature:
//
//	Patch(ctx, name, pt, data, opts, subresources...) (*v1.Service, error)
func (sp *servicePatcher) Patch(ctx context.Context, err error) error {
	if reflect.DeepEqual(sp.base.Annotations, sp.updated.Annotations) {
		return err
	}

	curJSON, err := json.Marshal(sp.base)
	if err != nil {
		return errors.Errorf("failed to serialize current service object: %v", err)
	}

	modJSON, err := json.Marshal(sp.updated)
	if err != nil {
		return errors.Errorf("failed to serialize modified service object: %v", err)
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(curJSON, modJSON, corev1.Service{})
	if err != nil {
		return errors.Errorf("failed to create 2-way merge patch: %v", err)
	}

	if len(patch) == 0 || string(patch) == "{}" {
		return nil
	}

	_, err = sp.kclient.CoreV1().Services(sp.updated.Namespace).Patch(
		context.TODO(),
		sp.updated.Name,
		types.StrategicMergePatchType,
		patch,
		metav1.PatchOptions{},
	)
	if err != nil {
		return errors.Errorf("failed to patch service object %s/%s: %v", sp.updated.Namespace, sp.updated.Name, err)
	}

	klog.V(4).Infof("servicePatcher: patched annotations on service %s/%s",
		sp.updated.Namespace, sp.updated.Name)

	return nil
}

// updateServiceAnnotation sets key=value on the Service's annotations using a
// servicePatcher and applies the patch.  Errors are logged but not returned —
// annotation recording is best-effort.
func (lbaas *LBaasV2) updateServiceAnnotation(service *corev1.Service, key, value string) {
	if service.Annotations == nil {
		service.Annotations = map[string]string{}
	}
	service.Annotations[key] = value
}
