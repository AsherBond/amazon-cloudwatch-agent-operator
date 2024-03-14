// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package eks_addon

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/instrumentation/auto"
	"k8s.io/apimachinery/pkg/labels"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	arv1 "k8s.io/api/admissionregistration/v1"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	rbacV1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	nameSpace        = "amazon-cloudwatch"
	addOnName        = "amazon-cloudwatch-observability"
	agentName        = "cloudwatch-agent"
	podNameRegex     = "(" + agentName + "|" + addOnName + "-controller-manager|fluent-bit)-*"
	serviceNameRegex = agentName + "(-headless|-monitoring)?|" + addOnName + "-webhook-service|" + "dcgm-exporter-service"
)

func TestOperatorOnEKs(t *testing.T) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("error getting user home dir: %v\n", err)
	}
	kubeConfigPath := filepath.Join(userHomeDir, ".kube", "config")
	t.Logf("Using kubeconfig: %s\n", kubeConfigPath)

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		t.Fatalf("Error getting kubernetes config: %v\n", err)
	}

	clientSet, err := kubernetes.NewForConfig(kubeConfig)

	if err != nil {
		t.Fatalf("error getting kubernetes config: %v\n", err)
	}

	// Validating the "amazon-cloudwatch" namespace creation as part of EKS addon
	namespace, err := GetNameSpace(nameSpace, clientSet)
	assert.NoError(t, err)
	assert.Equal(t, nameSpace, namespace.Name)

	//Validating the number of pods and status
	pods, err := ListPods(nameSpace, clientSet)
	assert.NoError(t, err)
	assert.Len(t, pods.Items, 3)
	for _, pod := range pods.Items {
		fmt.Println("pod name: " + pod.Name + " namespace:" + pod.Namespace)
		assert.Equal(t, v1.PodRunning, pod.Status.Phase)
		// matches
		// - cloudwatch-agent-*
		// - amazon-cloudwatch-observability-controller-manager-*
		// - fluent-bit-*
		if match, _ := regexp.MatchString(podNameRegex, pod.Name); !match {
			assert.Fail(t, "Cluster Pods are not created correctly")
		}
	}

	//Validating the services
	services, err := ListServices(nameSpace, clientSet)
	assert.NoError(t, err)
	assert.Len(t, services.Items, 5)
	for _, service := range services.Items {
		fmt.Println("service name: " + service.Name + " namespace:" + service.Namespace)
		// matches
		// - amazon-cloudwatch-observability-webhook-service
		// - cloudwatch-agent
		// - cloudwatch-agent-headless
		// - cloudwatch-agent-monitoring
		if match, _ := regexp.MatchString(serviceNameRegex, service.Name); !match {
			assert.Fail(t, "Cluster Service is not created correctly")
		}
	}

	//Validating the Deployment
	deployments, err := ListDeployments(nameSpace, clientSet)
	assert.NoError(t, err)
	for _, deployment := range deployments.Items {
		fmt.Println("deployment name: " + deployment.Name + " namespace:" + deployment.Namespace)
	}
	assert.Len(t, deployments.Items, 1)
	// matches
	// - amazon-cloudwatch-observability-controller-manager
	assert.Equal(t, addOnName+"-controller-manager", deployments.Items[0].Name)
	for _, deploymentCondition := range deployments.Items[0].Status.Conditions {
		fmt.Println("deployment condition type: " + deploymentCondition.Type)
	}
	assert.Equal(t, appsV1.DeploymentAvailable, deployments.Items[0].Status.Conditions[0].Type)

	//updating operator deployment
	args := deployments.Items[0].Spec.Template.Spec.Containers[0].Args
	fmt.Println("These are the args: ", args)
	indexOfAutoAnnotationConfigString := findMatchingPrefix("--auto-annotation-config=", args)

	annotationConfig := auto.AnnotationConfig{
		Java: auto.AnnotationResources{
			Namespaces:   []string{""},
			DaemonSets:   []string{""},
			Deployments:  []string{"default/nginx"},
			StatefulSets: []string{""},
		},
	}
	jsonStr, err := json.Marshal(annotationConfig)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	fmt.Println("This is hte index of annotation: ", indexOfAutoAnnotationConfigString)
	//if auto annotation not part of config, we will add it
	if indexOfAutoAnnotationConfigString < 0 || indexOfAutoAnnotationConfigString >= len(deployments.Items[0].Spec.Template.Spec.Containers[0].Args) {
		fmt.Println("We are in the if statement")
		deployments.Items[0].Spec.Template.Spec.Containers[0].Args = append(deployments.Items[0].Spec.Template.Spec.Containers[0].Args, "--auto-annotation-config="+string(jsonStr))
		indexOfAutoAnnotationConfigString = len(deployments.Items[0].Spec.Template.Spec.Containers[0].Args) - 1
		fmt.Println("AutoAnnotationConfiguration: " + deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString])
		fmt.Println("This is the updated index of annotation: ", indexOfAutoAnnotationConfigString)
	} else {
		fmt.Println("We are in the else statement")
		deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString] = "--auto-annotation-config=" + string(jsonStr)
		fmt.Println("AutoAnnotationConfiguration: " + deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString])

	}

	// Update operator Deployment
	_, err = clientSet.AppsV1().Deployments("amazon-cloudwatch").Update(context.TODO(), &deployments.Items[0], metav1.UpdateOptions{})
	if err != nil {
		fmt.Printf("Error updating Deployment: %s\n", err)
		os.Exit(1)
	}
	fmt.Println("Deployment updated successfully!")

	//check if deployment has annotations.
	deployment, err := clientSet.AppsV1().Deployments("default").Get(context.TODO(), "nginx", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get nginx deployment: %s", err.Error())
	}

	// List pods belonging to the nginx deployment
	set := labels.Set(deployment.Spec.Selector.MatchLabels)
	deploymentPods, err := clientSet.CoreV1().Pods(deployment.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	})
	if err != nil {
		t.Fatalf("Error listing pods for nginx deployment: %s", err.Error())
	}

	//wait for pods to update
	time.Sleep(10 * time.Second)

	for _, pod := range deploymentPods.Items {
		fmt.Println("This is the pod: ", pod, pod.ObjectMeta.Annotations)

		fmt.Printf("This is the key: %v, this is value: %v\n", "instrumentation.opentelemetry.io/inject-java", pod.ObjectMeta.Annotations["instrumentation.opentelemetry.io/inject-java"])
		fmt.Printf("This is the key: %v, this is value: %v\n", "cloudwatch.aws.amazon.com/auto-annotate-java", pod.ObjectMeta.Annotations["cloudwatch.aws.amazon.com/auto-annotate-java"])

		argMap, _ := getPodAnnotationVariables(clientSet, pod.Name, "default")
		fmt.Println("This is the argMap: ", argMap)
		//assert.Equal(t, "", pod.Annotations["cloudwatch.aws.amazon.com/auto-annotate-java"], "Pod %s in namespace %s does not have cloudwatch annotation", pod.Name, pod.Namespace)
		assert.Equal(t, "", pod.Annotations["instrumentation.opentelemetry.io/inject-java"], "Pod %s in namespace %s does not have opentelemetry annotation", pod.Name, pod.Namespace)
		assert.Equal(t, "", pod.Annotations["cloudwatch.aws.amazon.com/auto-annotate-java"], "Pod %s in namespace %s does not have opentelemetry annotation", pod.Name, pod.Namespace)

	}

	fmt.Printf("All nginx pods have the correct annotations\n")
	if err != nil {
		t.Fatalf("Error listing pods: %s", err.Error())
	}
	//
	//annotationConfig = auto.AnnotationConfig{
	//	Java: auto.AnnotationResources{
	//		Namespaces:   []string{""},
	//		DaemonSets:   []string{"amazon-cloudwatch/fluent-bit"},
	//		Deployments:  []string{""},
	//		StatefulSets: []string{""},
	//	},
	//}
	//// Get the fluent-bit DaemonSet
	//daemonSet, err := clientSet.AppsV1().DaemonSets("amazon-cloudwatch").Get(context.TODO(), "fluent-bit", metav1.GetOptions{})
	//if err != nil {
	//	t.Fatalf("Failed to get fluent-bit daemonset: %s", err.Error())
	//}
	//
	//// List pods belonging to the fluent-bit DaemonSet
	//set = labels.Set(daemonSet.Spec.Selector.MatchLabels)
	//daemonPods, err := clientSet.CoreV1().Pods(daemonSet.Namespace).List(context.TODO(), metav1.ListOptions{
	//	LabelSelector: set.AsSelector().String(),
	//})
	//if err != nil {
	//	t.Fatalf("Error listing pods for fluent-bit daemonset: %s", err.Error())
	//}
	//// Update the Deployment
	//_, err = clientSet.AppsV1().Deployments("amazon-cloudwatch").Update(context.TODO(), &deployments.Items[0], metav1.UpdateOptions{})
	//if err != nil {
	//	fmt.Printf("Error updating Deployment: %s\n", err)
	//	os.Exit(1)
	//}
	//fmt.Println("Deployment updated successfully!")
	//
	//for _, pod := range daemonPods.Items {
	//	assert.Equal(t, "true", pod.Annotations["cloudwatch.aws.amazon.com/auto-annotate-java"], "Pod %s in namespace %s does not have cloudwatch annotation", pod.Name, pod.Namespace)
	//	assert.Equal(t, "true", pod.Annotations["instrumentation.opentelemetry.io/inject-java"], "Pod %s in namespace %s does not have opentelemetry annotation", pod.Name, pod.Namespace)
	//}
	//
	//fmt.Printf("All fluent-bit pods have the correct annotations\n")

	//Validating the Daemon Sets
	daemonSets, err := ListDaemonSets(nameSpace, clientSet)
	assert.NoError(t, err)
	assert.Len(t, daemonSets.Items, 3)
	for _, daemonSet := range daemonSets.Items {
		fmt.Println("daemonSet name: " + daemonSet.Name + " namespace:" + daemonSet.Namespace)
		// matches
		// - cloudwatch-agent
		// - fluent-bit
		if match, _ := regexp.MatchString(agentName+"|fluent-bit", daemonSet.Name); !match {
			assert.Fail(t, "DaemonSet is created correctly")
		}
	}

	// Validating Service Accounts
	serviceAccounts, err := ListServiceAccounts(nameSpace, clientSet)
	assert.NoError(t, err)
	for _, sa := range serviceAccounts.Items {
		fmt.Println("serviceAccounts name: " + sa.Name + " namespace:" + sa.Namespace)
	}
	// searches
	// - amazon-cloudwatch-observability-controller-manager
	// - cloudwatch-agent
	assert.True(t, validateServiceAccount(serviceAccounts, addOnName+"-controller-manager"))
	assert.True(t, validateServiceAccount(serviceAccounts, agentName))

	//Validating ClusterRoles
	clusterRoles, err := ListClusterRoles(clientSet)
	assert.NoError(t, err)
	// searches
	// - amazon-cloudwatch-observability-manager-role
	// - cloudwatch-agent-role
	assert.True(t, validateClusterRoles(clusterRoles, addOnName+"-manager-role"))
	assert.True(t, validateClusterRoles(clusterRoles, agentName+"-role"))

	//Validating ClusterRoleBinding
	clusterRoleBindings, err := ListClusterRoleBindings(clientSet)
	assert.NoError(t, err)
	// searches
	// - amazon-cloudwatch-observability-manager-rolebinding
	// - cloudwatch-agent-role-binding
	assert.True(t, validateClusterRoleBindings(clusterRoleBindings, addOnName+"-manager-rolebinding"))
	assert.True(t, validateClusterRoleBindings(clusterRoleBindings, agentName+"-role-binding"))

	//Validating MutatingWebhookConfiguration
	mutatingWebhookConfigurations, err := ListMutatingWebhookConfigurations(clientSet)
	assert.NoError(t, err)
	assert.Len(t, mutatingWebhookConfigurations.Items[0].Webhooks, 3)
	// searches
	// - amazon-cloudwatch-observability-mutating-webhook-configuration
	assert.Equal(t, addOnName+"-mutating-webhook-configuration", mutatingWebhookConfigurations.Items[0].Name)

	//Validating ValidatingWebhookConfiguration
	validatingWebhookConfigurations, err := ListValidatingWebhookConfigurations(clientSet)
	assert.NoError(t, err)
	assert.Len(t, validatingWebhookConfigurations.Items[0].Webhooks, 4)
	// searches
	// - amazon-cloudwatch-observability-validating-webhook-configuration
	assert.Equal(t, addOnName+"-validating-webhook-configuration", validatingWebhookConfigurations.Items[0].Name)
}

//	func updateDeployment(annotationConfig auto.AnnotationConfig, deployments *v1.DeploymentList) {
//		jsonStr, err := json.Marshal(annotationConfig)
//		if err != nil {
//			fmt.Println("Error:", err)
//			return
//		}
//
//		deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString] = "--auto-annotation-config=" + string(jsonStr)
//		fmt.Println("AutoAnnotationConfiguration: " + deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString])
//
//		// Update the Deployment
//		_, err = clientSet.AppsV1().Deployments("amazon-cloudwatch").Update(context.TODO(), &deployments.Items[0], metav1.UpdateOptions{})
//		if err != nil {
//			fmt.Printf("Error updating Deployment: %s\n", err)
//			os.Exit(1)
//		}
//		fmt.Println("Deployment updated successfully!")
//
// }

func waitForDeploymentReady(clientSet *kubernetes.Clientset, namespace string, deploymentName string, timeout time.Duration) error {
	start := time.Now()
	for {
		if time.Since(start) > timeout {
			return fmt.Errorf("timed out waiting for Deployment readiness")
		}

		dep, err := clientSet.AppsV1().Deployments(namespace).Get(context.TODO(), deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if dep.Status.Replicas == dep.Status.ReadyReplicas &&
			dep.Status.Replicas == dep.Status.UpdatedReplicas &&
			dep.Status.Replicas == *dep.Spec.Replicas {
			fmt.Println("Deployment is ready")
			return nil
		}

		time.Sleep(10 * time.Second) // Poll interval
	}
}

func getPodAnnotationVariables(clientset *kubernetes.Clientset, podName, namespace string) (map[string]string, error) {
	pod, err := clientset.CoreV1().Pods("default").Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	argMap := make(map[string]string)

	for _, container := range pod.Spec.Containers {
		for _, argVar := range container.Args {
			argMap[argVar] = argVar
		}
	}

	return argMap, nil
}
func findMatchingPrefix(str string, strs []string) int {
	for i, s := range strs {
		if strings.HasPrefix(s, str) {
			return i
		}
	}
	return -1 // Return -1 if no matching prefix is found
}
func validateServiceAccount(serviceAccounts *v1.ServiceAccountList, serviceAccountName string) bool {
	for _, serviceAccount := range serviceAccounts.Items {
		if serviceAccount.Name == serviceAccountName {
			return true
		}
	}
	return false
}

func validateClusterRoles(clusterRoles *rbacV1.ClusterRoleList, clusterRoleName string) bool {
	for _, clusterRole := range clusterRoles.Items {
		if clusterRole.Name == clusterRoleName {
			return true
		}
	}
	return false
}

func validateClusterRoleBindings(clusterRoleBindings *rbacV1.ClusterRoleBindingList, clusterRoleBindingName string) bool {
	for _, clusterRoleBinding := range clusterRoleBindings.Items {
		if clusterRoleBinding.Name == clusterRoleBindingName {
			return true
		}
	}
	return false
}

func ListPods(namespace string, client kubernetes.Interface) (*v1.PodList, error) {
	pods, err := client.CoreV1().Pods(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting pods: %v\n", err)
		return nil, err
	}
	return pods, nil
}

func GetNameSpace(namespace string, client kubernetes.Interface) (*v1.Namespace, error) {
	ns, err := client.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	if err != nil {
		err = fmt.Errorf("error getting namespace: %v\n", err)
		return nil, err
	}
	return ns, nil
}

func ListServices(namespace string, client kubernetes.Interface) (*v1.ServiceList, error) {
	namespaces, err := client.CoreV1().Services(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting Services: %v\n", err)
		return nil, err
	}
	return namespaces, nil
}

func ListDeployments(namespace string, client kubernetes.Interface) (*appsV1.DeploymentList, error) {
	deployments, err := client.AppsV1().Deployments(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting Deploymets: %v\n", err)
		return nil, err
	}
	return deployments, nil
}

func ListDaemonSets(namespace string, client kubernetes.Interface) (*appsV1.DaemonSetList, error) {
	daemonSets, err := client.AppsV1().DaemonSets(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting DaemonSets: %v\n", err)
		return nil, err
	}
	return daemonSets, nil
}

func ListServiceAccounts(namespace string, client kubernetes.Interface) (*v1.ServiceAccountList, error) {
	serviceAccounts, err := client.CoreV1().ServiceAccounts(namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting ServiceAccounts: %v\n", err)
		return nil, err
	}
	return serviceAccounts, nil
}

func ListClusterRoles(client kubernetes.Interface) (*rbacV1.ClusterRoleList, error) {
	clusterRoles, err := client.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting ClusterRoles: %v\n", err)
		return nil, err
	}
	return clusterRoles, nil
}

func ListClusterRoleBindings(client kubernetes.Interface) (*rbacV1.ClusterRoleBindingList, error) {
	clusterRoleBindings, err := client.RbacV1().ClusterRoleBindings().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting ClusterRoleBindings: %v\n", err)
		return nil, err
	}
	return clusterRoleBindings, nil
}

func ListMutatingWebhookConfigurations(client kubernetes.Interface) (*arv1.MutatingWebhookConfigurationList, error) {
	mutatingWebhookConfigurations, err := client.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting MutatingWebhookConfigurations: %v\n", err)
		return nil, err
	}
	return mutatingWebhookConfigurations, nil
}

func ListValidatingWebhookConfigurations(client kubernetes.Interface) (*arv1.ValidatingWebhookConfigurationList, error) {
	validatingWebhookConfigurations, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting ValidatingWebhookConfigurations: %v\n", err)
		return nil, err
	}
	return validatingWebhookConfigurations, nil
}
