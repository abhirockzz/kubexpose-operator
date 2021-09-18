/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubexposev1 "github.com/abhirockzz/kubexpose-operator/api/v1"
)

// KubexposeReconciler reconciles a Kubexpose object
type KubexposeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	serviceNameFormat    string = "%s-svc-%s"
	deploymentNameFormat string = "%s-expose-%s"
)

//+kubebuilder:rbac:groups=kubexpose.kubexpose.io,resources=kubexposes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kubexpose.kubexpose.io,resources=kubexposes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kubexpose.kubexpose.io,resources=kubexposes/finalizers,verbs=update

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete

// kubexpose also needs to exec into the pod
// +kubebuilder:rbac:groups=core,resources=pods/exec,verbs=create

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *KubexposeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.Log.WithValues("kubexpose", req.NamespacedName)
	logger.Info("reconciling resource")

	var kubexposeResource kubexposev1.Kubexpose
	err := r.Get(ctx, req.NamespacedName, &kubexposeResource)

	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("kubexpose resource not found. ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}

		logger.Error(err, "failed to get kubexpose resource")
		return ctrl.Result{}, err
	}

	// check for Service and create one if it does not exist
	serviceName := fmt.Sprintf(serviceNameFormat, kubexposeResource.Spec.SourceDeploymentName, kubexposeResource.Name)
	namespace := kubexposeResource.Spec.TargetNamespace

	var kubexposeService corev1.Service
	err = r.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: serviceName}, &kubexposeService)

	if err != nil {
		if errors.IsNotFound(err) {
			return r.createService(ctx, req, &kubexposeResource)
		} else {
			logger.Error(err, "failed to get Service")
			return ctrl.Result{}, err
		}
	}

	// check for Deployment and create one if it does not exist
	deploymentName := fmt.Sprintf(deploymentNameFormat, kubexposeResource.Spec.SourceDeploymentName, kubexposeResource.Name)

	var ngrokDeployment appsv1.Deployment
	err = r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: deploymentName}, &ngrokDeployment)

	if err != nil {
		if errors.IsNotFound(err) {
			return r.createDeployment(ctx, req, &kubexposeResource)
		} else {
			logger.Error(err, "failed to get deployment")
			return ctrl.Result{}, err
		}
	}

	statusURL := kubexposeResource.Status.PublicURL
	logger.Info("url as per status", "kubexpose resource", kubexposeResource.Name, "url", statusURL)

	latestNgrokURL, err := r.getURL(ctx, req, &kubexposeResource)
	if err != nil {
		// there will be intermittent errors when trying to search for url.
		// logging it as info to avoid console pollution
		logger.Info("error fetching public url", "error", err.Error())
		// we are using nil instead of err below
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// if they are not same, update the status with the new URL in deployment
	if statusURL != latestNgrokURL {
		kubexposeResource.Status.PublicURL = latestNgrokURL
		return r.updateStatus(ctx, req, &kubexposeResource)
	}

	logger.Info("resource successfully reconciled", "service", serviceName, "deployment", deploymentName, "public url", kubexposeResource.Status.PublicURL)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubexposeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubexposev1.Kubexpose{}).
		// will reconcile the service if it's modified/deleted externally
		Owns(&corev1.Service{}).
		// will reconcile the deployment if it's modified/deleted externally
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
