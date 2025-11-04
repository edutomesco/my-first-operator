/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	snapshotv1alpha1 "github.com/edutomesco/my-first-operator.git/api/v1alpha1"
)

// WbcSnapshotReconciler reconciles a WbcSnapshot object
type WbcSnapshotReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=snapshot.wbc.com,resources=wbcsnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.wbc.com,resources=wbcsnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=snapshot.wbc.com,resources=wbcsnapshots/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the WbcSnapshot object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *WbcSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	snapshot := &snapshotv1alpha1.WbcSnapshot{}
	snapshotReadErr := r.Get(ctx, req.NamespacedName, snapshot)
	if snapshotReadErr != nil || len(snapshot.Name) == 0 {
		logf.Log.Error(snapshotReadErr, "Error encountered reading WbcSnapshot")
		return ctrl.Result{}, snapshotReadErr
	}

	snapshotCreator := &v1.Pod{}
	snapshotCreatorErr := r.Get(
		ctx, types.NamespacedName{Name: "snapshot-creator", Namespace: req.Namespace}, snapshotCreator,
	)
	if snapshotCreatorErr == nil {
		logf.Log.Error(snapshotCreatorErr, "Snapshot Creator already running!")
		return ctrl.Result{}, snapshotCreatorErr
	}

	newPvName := "wbc-snapshot-pv-" + strconv.FormatInt(time.Now().Unix(), 10) + "-" + snapshot.Spec.SourceVolumeName
	newPvcName := "wbc-snapshot-pvc-" + strconv.FormatInt(time.Now().Unix(), 10) + "-" + snapshot.Spec.SourceClaimName

	newPersistentVol := v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newPvName,
			Namespace: req.Namespace,
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName: "manual",
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Capacity: v1.ResourceList{
				v1.ResourceStorage: resource.MustParse("10Gi"),
			},
			PersistentVolumeSource: v1.PersistentVolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: snapshot.Spec.HostPath,
				},
			},
		},
	}

	pvCreateErr := r.Create(ctx, &newPersistentVol)
	if pvCreateErr != nil {
		logf.Log.Error(pvCreateErr, "Error encountered creating new pv")
		return ctrl.Result{}, pvCreateErr
	}

	manualStorageClass := "manual"

	newPersistentVolumeClaim := v1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      newPvcName,
			Namespace: req.Namespace,
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: &manualStorageClass,
			VolumeName:       newPvName,
			AccessModes:      []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
			Resources: v1.VolumeResourceRequirements{
				Requests: v1.ResourceList{
					v1.ResourceStorage: resource.MustParse("10Gi"),
				},
			},
		},
	}

	pvcCreateErr := r.Create(ctx, &newPersistentVolumeClaim)
	if pvcCreateErr != nil {
		logf.Log.Error(pvcCreateErr, "Error encountered creating new pvc")
		return ctrl.Result{}, pvcCreateErr
	}

	snapshotCreatorPod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "snapshot-creator",
			Namespace: req.Namespace,
		},
		Spec: v1.PodSpec{
			RestartPolicy: "Never",
			Volumes: []v1.Volume{
				{
					Name: "wbc-snapshot-" + snapshot.Spec.SourceClaimName,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: snapshot.Spec.SourceClaimName,
						},
					},
				},
				{
					Name: "wbc-snapshot-" + newPvcName,
					VolumeSource: v1.VolumeSource{
						PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
							ClaimName: newPvcName,
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  "busybox",
					Image: "k8s.grc.io/busybox",
					Command: []string{
						"/bin/sh",
						"-c",
						"cp /tmp/source/test.file /tmp/dest/test.file",
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      "wbc-snapshot-" + snapshot.Spec.SourceClaimName,
							MountPath: "/tmp/source",
						},
						{
							Name:      "wbc-snapshot" + newPvcName,
							MountPath: "/tmp/dest",
						},
					},
				},
			},
		},
	}

	podCreatorErr := r.Create(ctx, &snapshotCreatorPod)
	if podCreatorErr != nil {
		logf.Log.Error(podCreatorErr, "Error encountered creating snapshot pod")
		return ctrl.Result{}, pvcCreateErr
	}

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WbcSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&snapshotv1alpha1.WbcSnapshot{}).
		Named("wbcsnapshot").
		Complete(r)
}
