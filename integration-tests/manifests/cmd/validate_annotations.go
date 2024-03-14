package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aws/amazon-cloudwatch-agent-operator/pkg/instrumentation/auto"
	appsV1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {

	args := os.Args
	namespace := args[1]

	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("error getting user home dir: %v\n\n", err)
	}
	kubeConfigPath := filepath.Join(userHomeDir, ".kube", "config")
	fmt.Printf("Using kubeconfig: %s\n\n", kubeConfigPath)

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		fmt.Printf("Error getting kubernetes config: %v\n\n", err)
	}

	clientSet, err := kubernetes.NewForConfig(kubeConfig)

	if err != nil {
		fmt.Printf("error getting kubernetes config: %v\n\n", err)
	}
	deployments, err := ListDeployments(namespace, clientSet)

	success := verifyAutoAnnotation(deployments, clientSet)
	if !success {
		fmt.Println("Instrumentation Annotation Injection Test: FAIL")
		os.Exit(1)
	} else {
		fmt.Println("Instrumentation Annotation Injection Test: PASS")
	}
}

func verifyAutoAnnotation(deployments *appsV1.DeploymentList, clientSet *kubernetes.Clientset) bool {

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
		return false
	}

	//finding where index of --auto-annotation-config= is (if it doesn't exist it will be appended)
	indexOfAutoAnnotationConfigString = updateAnnotationConfig(indexOfAutoAnnotationConfigString, deployments, string(jsonStr))
	fmt.Println("This is the index of annotation: ", indexOfAutoAnnotationConfigString)
	if !updateOperator(clientSet, deployments) {
		return false
	}
	time.Sleep(10 * time.Second)

	//check if deployment has annotations.
	deployment, err := clientSet.AppsV1().Deployments("default").Get(context.TODO(), "nginx", metav1.GetOptions{})
	if err != nil {
		fmt.Println("Failed to get nginx deployment: %s", err.Error())
		return false
	}

	// List pods belonging to the nginx deployment
	set := labels.Set(deployment.Spec.Selector.MatchLabels)
	deploymentPods, err := clientSet.CoreV1().Pods(deployment.Namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	})
	if err != nil {
		fmt.Println("Error listing pods for nginx deployment: %s", err.Error())
		return false
	}

	//wait for pods to update

	if !checkIfAnnotationsExist(deploymentPods) {
		return false
	}

	annotationConfig = auto.AnnotationConfig{
		Java: auto.AnnotationResources{
			Namespaces:   []string{""},
			DaemonSets:   []string{"default/fluent-bit"},
			Deployments:  []string{""},
			StatefulSets: []string{""},
		},
	}
	jsonStr, err = json.Marshal(annotationConfig)
	if err != nil {
		fmt.Println("Error:", err)
		return false
	}
	indexOfAutoAnnotationConfigString = updateAnnotationConfig(indexOfAutoAnnotationConfigString, deployments, string(jsonStr))
	if !updateOperator(clientSet, deployments) {
		fmt.Println("kasjdfkls")
		return false
	}
	time.Sleep(10 * time.Second)

	// Get the fluent-bit DaemonSet
	daemonSet, err := clientSet.AppsV1().DaemonSets("default").Get(context.TODO(), "fluent-bit", metav1.GetOptions{})
	if err != nil {
		fmt.Println("Failed to get fluent-bit daemonset: %s", err.Error())
	}

	// List pods belonging to the fluent-bit DaemonSet
	set = labels.Set(daemonSet.Spec.Selector.MatchLabels)
	daemonPods, err := clientSet.CoreV1().Pods("amazon-cloudwatch").List(context.TODO(), metav1.ListOptions{
		LabelSelector: set.AsSelector().String(),
	})
	if err != nil {
		fmt.Println("Error listing pods for fluent-bit daemonset: %s", err.Error())
	}

	if !checkIfAnnotationsExist(daemonPods) {
		return false
	}
	fmt.Printf("All fluent-bit pods have the correct annotations\n")
	return true
}

func updateOperator(clientSet *kubernetes.Clientset, deployments *appsV1.DeploymentList) bool {

	// Update operator Deployment
	_, err := clientSet.AppsV1().Deployments("amazon-cloudwatch").Update(context.TODO(), &deployments.Items[0], metav1.UpdateOptions{})
	if err != nil {
		fmt.Printf("Error updating Deployment: %s\n", err)
		return false
	}
	fmt.Println("Deployment updated successfully!")
	return true
}

func checkIfAnnotationsExist(deploymentPods *v1.PodList) bool {
	for _, pod := range deploymentPods.Items {

		fmt.Printf("This is the key: %v, this is value: %v\n", "instrumentation.opentelemetry.io/inject-java", pod.ObjectMeta.Annotations["instrumentation.opentelemetry.io/inject-java"])

		if pod.ObjectMeta.Annotations["instrumentation.opentelemetry.io/inject-java"] != "true" {
			return false
		}
		if pod.ObjectMeta.Annotations["cloudwatch.aws.amazon.com/auto-annotate-java"] != "true" {
			return false
		}

	}

	fmt.Printf("All nginx pods have the correct annotations\n")
	return true
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

func updateAnnotationConfig(indexOfAutoAnnotationConfigString int, deployments *appsV1.DeploymentList, jsonStr string) int {
	//if auto annotation not part of config, we will add it
	if indexOfAutoAnnotationConfigString < 0 || indexOfAutoAnnotationConfigString >= len(deployments.Items[0].Spec.Template.Spec.Containers[0].Args) {
		fmt.Println("We are in the if statement")
		deployments.Items[0].Spec.Template.Spec.Containers[0].Args = append(deployments.Items[0].Spec.Template.Spec.Containers[0].Args, "--auto-annotation-config="+jsonStr)
		indexOfAutoAnnotationConfigString = len(deployments.Items[0].Spec.Template.Spec.Containers[0].Args) - 1
		fmt.Println("AutoAnnotationConfiguration: " + deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString])
		fmt.Println("This is the updated index of annotation: ", indexOfAutoAnnotationConfigString)
	} else {
		fmt.Println("We are in the else statement")
		deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString] = "--auto-annotation-config=" + jsonStr
		fmt.Println("AutoAnnotationConfiguration: " + deployments.Items[0].Spec.Template.Spec.Containers[0].Args[indexOfAutoAnnotationConfigString])
	}
	return indexOfAutoAnnotationConfigString
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

func findMatchingPrefix(str string, strs []string) int {
	for i, s := range strs {
		if strings.HasPrefix(s, str) {
			return i
		}
	}
	return -1 // Return -1 if no matching prefix is found
}
