package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	stderror "errors"

	kubexposev1 "github.com/abhirockzz/kubexpose-operator/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// createService creates a Service (type ClusterIP) for the Deployment to be accessed
func (r *KubexposeReconciler) createService(ctx context.Context, req ctrl.Request, kexp *kubexposev1.Kubexpose) (ctrl.Result, error) {
	logger := log.Log.WithValues("kubexpose", req.NamespacedName)

	namespace := kexp.Spec.TargetNamespace

	logger.Info("looking for source deployment", "namespace", namespace, "name", kexp.Spec.SourceDeploymentName)

	// we need the Deployment labels to create the Service
	var sourceDeployment appsv1.Deployment
	err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: kexp.Spec.SourceDeploymentName}, &sourceDeployment)

	if err != nil {
		if errors.IsNotFound(err) {
			logger.Error(err, "source deployment does not exist", "namespace", namespace, "name", kexp.Spec.SourceDeploymentName)
		} else {
			logger.Error(err, "error finding source deployment", "namespace", namespace, "name", kexp.Spec.SourceDeploymentName)
		}

		// can't do much here. do not requeue
		return ctrl.Result{}, nil
	}

	logger.Info("found source deployment", "namespace", namespace, "name", kexp.Spec.SourceDeploymentName)

	serviceName := fmt.Sprintf(serviceNameFormat, sourceDeployment.Name, kexp.Name)
	selector := sourceDeployment.Spec.Selector.MatchLabels

	svc := &corev1.Service{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector,
			Ports: []corev1.ServicePort{
				{
					Port: int32(kexp.Spec.PortToExpose),
				},
			},
		},
	}
	// Set Kubexpose instance as the owner and controller
	err = ctrl.SetControllerReference(kexp, svc, r.Scheme)

	if err != nil {
		logger.Error(err, "error setting controller reference", "namespace", svc.Namespace, "name", svc.Name)

		// note - this will requeue
		return ctrl.Result{}, err
	}

	logger.Info("initiating service creation", "namespace", svc.Namespace, "name", svc.Name)

	err = r.Create(ctx, svc)

	if err != nil {
		logger.Error(err, "failed to create service", "namespace", svc.Namespace, "name", svc.Name)

		// note - this will requeue
		return ctrl.Result{}, err
	}

	logger.Info("service successfully created", "namespace", svc.Namespace, "name", svc.Name)

	// service created successfully - return and requeue
	return ctrl.Result{Requeue: true}, nil
}

// createDeployment creates a ngrok Deployment
func (r *KubexposeReconciler) createDeployment(ctx context.Context, req ctrl.Request, kexp *kubexposev1.Kubexpose) (ctrl.Result, error) {
	logger := log.Log.WithValues("kubexpose", req.NamespacedName)
	namespace := kexp.Spec.TargetNamespace

	deploymentName := fmt.Sprintf(deploymentNameFormat, kexp.Spec.SourceDeploymentName, kexp.Name)

	numReplicas := int32(1)
	serviceName := fmt.Sprintf(serviceNameFormat, kexp.Spec.SourceDeploymentName, kexp.Name)

	ngrokPort := int32(4040)

	dep := &appsv1.Deployment{
		ObjectMeta: metaV1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &numReplicas,
			Selector: &metaV1.LabelSelector{
				MatchLabels: map[string]string{
					"exposing":     kexp.Spec.SourceDeploymentName,
					"kubexpose-cr": kexp.Name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metaV1.ObjectMeta{
					Labels: map[string]string{
						"exposing":     kexp.Spec.SourceDeploymentName,
						"kubexpose-cr": kexp.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "ngrok",
							Image:   "wernight/ngrok",
							Command: []string{"ngrok"},
							Args:    []string{"http", serviceName + ":" + strconv.Itoa(kexp.Spec.PortToExpose)},
							Ports:   []corev1.ContainerPort{{ContainerPort: ngrokPort}},
						},
					},
				},
			},
		},
	}

	// Set Kubexpose instance as the owner and controller
	err := ctrl.SetControllerReference(kexp, dep, r.Scheme)

	if err != nil {
		logger.Error(err, "error setting controller reference", "namespace", dep.Namespace, "name", dep.Name)

		// note - this will requeue
		return ctrl.Result{}, err
	}

	logger.Info("initiating new deployment creation", "namespace", dep.Namespace, "name", dep.Name)

	err = r.Create(ctx, dep)

	if err != nil {
		logger.Error(err, "failed to create deployment", "namespace", dep.Namespace, "name", dep.Name)

		// note - this will requeue
		return ctrl.Result{}, err
	}

	logger.Info("deployment created successfully", "namespace", dep.Namespace, "name", dep.Name)

	// Deployment created successfully - return and requeue
	return ctrl.Result{Requeue: true}, nil
}

func (r *KubexposeReconciler) getURL(ctx context.Context, req ctrl.Request, kexp *kubexposev1.Kubexpose) (string, error) {
	logger := log.Log.WithValues("kubexpose", req.NamespacedName)

	logger.Info("fetching url at which deployment will be accessible")

	cfg, err := config.GetConfig()
	if err != nil {
		//logger.Error(err, "failed to get rest config")
		return "", err
	}

	cfg.APIPath = "/api"
	cfg.GroupVersion = &schema.GroupVersion{Group: "", Version: "v1"}
	cfg.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{}

	var pods corev1.PodList
	selectorLabels := "exposing=" + kexp.Spec.SourceDeploymentName + ",kubexpose-cr=" + kexp.Name

	//logger.Info("looking for pod", "labels", selectorLabels)

	r1, _ := labels.NewRequirement("exposing", selection.Equals, []string{kexp.Spec.SourceDeploymentName})
	r2, _ := labels.NewRequirement("kubexpose-cr", selection.Equals, []string{kexp.Name})

	err = r.List(ctx, &pods, &client.ListOptions{LabelSelector: labels.NewSelector().Add(*r1, *r2), Namespace: kexp.Spec.TargetNamespace})

	if err != nil {
		//logger.Error(err, "failed to list pods")
		return "", err
	}

	if len(pods.Items) == 0 {
		//logger.Info("no pods found")
		return "", stderror.New("no pods found")
	}

	// we expect to get ONE pod only. there might be a situation when a Pod with same label might be terminating. we want retry in this case
	if len(pods.Items) > 1 {
		//logger.Info("multiple pods found!", "labels", selectorLabels)
		return "", stderror.New("multiple pods found for label - " + selectorLabels)
	}

	podName := pods.Items[0].Name

	//logger.Info("found pod", "Name", podName)

	restClient, err := rest.RESTClientFor(cfg)
	if err != nil {
		//logger.Error(err, "failed to get rest client")
		return "", err
	}

	namespace := kexp.Spec.TargetNamespace

	execReq := restClient.Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "ngrok",
			Command:   []string{"curl", "http://localhost:4040/api/tunnels"},
			//Stdin:     true,
			Stdout: true,
			Stderr: true,
			TTY:    false,
		}, runtime.NewParameterCodec(r.Scheme))

	executor, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, execReq.URL())

	if err != nil {
		//logger.Error(err, "failed to create command executor")
		return "", err
	}

	logger.Info("initiating 'exec' request", "url", execReq.URL())

	var stdout, stderr bytes.Buffer

	err = executor.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		//Tty:               false,
		//TerminalSizeQueue: nil,
	})

	if err != nil {
		//logger.Error(err, "failed to exec command")
		if strings.Contains(err.Error(), "container not found") {
			// ngrok container is not ready. give it a while
			return "", err
		}
		return "", err
	}

	if stdout.Bytes() == nil {
		//logger.Error(err, "no response from command")
		return "", err
	}

	var ngrokInfo NgrokInfo
	err = json.Unmarshal(stdout.Bytes(), &ngrokInfo)

	if err != nil {
		//logger.Error(err, "ngrok info unmarshal failed")
		return "", nil
	}

	// ngrok container is not ready. give it a while
	if len(ngrokInfo.Tunnels) == 0 {
		return "", stderror.New("ngrok container is not ready")
	}

	var url string

	// we only need https url
	if ngrokInfo.Tunnels[0].Proto == "https" {
		url = ngrokInfo.Tunnels[0].PublicURL
	} else {
		url = ngrokInfo.Tunnels[1].PublicURL
	}
	logger.Info("public url - " + url)
	return url, nil

}

// json response for ngrok info - curl http://localhost:4040/tunnels
type NgrokInfo struct {
	Tunnels []struct {
		PublicURL string `json:"public_url"`
		Proto     string `json:"proto"`
	} `json:"tunnels"`
}

func (r *KubexposeReconciler) updateStatus(ctx context.Context, req ctrl.Request, kexp *kubexposev1.Kubexpose) (ctrl.Result, error) {
	logger := log.Log.WithValues("kubexpose", req.NamespacedName)

	err := r.Status().Update(ctx, kexp)
	if err != nil {
		logger.Error(err, "failed to update  status with public url info")
		return ctrl.Result{}, err
	}

	logger.Info(kexp.Name + " status updated with public url info")

	return ctrl.Result{}, nil
}
