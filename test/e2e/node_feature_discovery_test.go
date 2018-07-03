/*
Copyright 2016 The Kubernetes Authors.

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

/*
  This code is a trial attempt to run e2e test without k8s/e2e framework,
  using k8s.io/client-go and k8s.io/apimachinery.
  This code currently contains devel-time stuff, like alternate local registry references,
  devel-time imageTag, debug prints etc, so it needs cleaning up.
  The operation ideas are not clear/complete  yet, see TODO about sync/async return of pod create.

  This code expects k8s cluster configured and running.
  Cluster config is retrieved from HOME/.kube/config.

  This code was developed and tried on CentOS-7 and k8s version v1.10.2

  Run it like this at main level:
  go test -v ./test/e2e/...

*/

package e2e

import (
	"os"
	"path"
	//"flag"
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	configPath = path.Join(os.Getenv("HOME"), "/.kube/config")
)

func newPod(nodeName string, Command []string) *v1.Pod {
	const (
		// released version:
		registry = "quay.io"
		imageTag = "v0.2.0"
		// local devel override:
		//registry          = "192.168.175.11:5000"
		//imageTag          = "v0.1.0-52-g01e2110-dirty"
	)
	//podName := "node-feature-discovery-" + string(uuid.NewUUID())
	podName := "nfd-" + string(uuid.NewUUID())
	image := fmt.Sprintf("%s/kubernetes_incubator/node-feature-discovery:%s", registry, imageTag)
	fmt.Printf("Create pod %s on node %s, image %s\n", podName, nodeName, image)
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			NodeName: nodeName,
			Containers: []v1.Container{
				{
					Name:    podName,
					Image:   image,
					Command: Command,
					Env: []v1.EnvVar{
						{
							Name: "NODE_NAME",
							ValueFrom: &v1.EnvVarSource{
								FieldRef: &v1.ObjectFieldSelector{
									FieldPath: "spec.nodeName",
								},
							},
						},
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}
	return pod
}

func TestNFD(t *testing.T) {
	const (
		//labelPrefix       = "node.alpha.intel.com/nfd"
		labelPrefix       = "node.alpha.kubernetes-incubator.io/nfd"
		fakeFeatureSource = "fake"
		// LabelNodeRoleMaster specifies that a node is a master
		LabelNodeRoleMaster = "node-role.kubernetes.io/master"
	)
	fakeFeatureLabels := map[string]string{
		fmt.Sprintf("%s-%s-fakefeature1", labelPrefix, fakeFeatureSource): "true",
		fmt.Sprintf("%s-%s-fakefeature2", labelPrefix, fakeFeatureSource): "true",
		fmt.Sprintf("%s-%s-fakefeature3", labelPrefix, fakeFeatureSource): "true",
	}
	var runNode, nameSpace string
	var GiveLabelsCommand = []string{"/go/bin/node-feature-discovery", "--sources=fake", "--oneshot"}
	var ClearLabelsCommand = []string{"/go/bin/node-feature-discovery", "--sources=fake", "--oneshot", "--label-whitelist=xyz"}
	//var GiveLabelsCommand  = []string{"/go/bin/node-feature-discovery", "--sources=fake"}
	//var ClearLabelsCommand = []string{"/go/bin/node-feature-discovery", "--sources=fake", "--label-whitelist=xyz"}

	// use the current context in kubeconfig
	config, _ := clientcmd.BuildConfigFromFlags("", configPath)
	// create the clientset
	clientSet, _ := clientset.NewForConfig(config)
	// access the API to list pods

	//pods, _ := clientSet.CoreV1().Pods("").List(metav1.ListOptions{})
	//fmt.Printf("There are %d pods in the cluster\n", len(pods.Items))

	nodes, _ := clientSet.CoreV1().Nodes().List(metav1.ListOptions{})
	fmt.Printf("There are %d nodes in the cluster\n", len(nodes.Items))

Outerloop:
	for _, n := range nodes.Items {
		fmt.Printf("check node [%s]\n", n.Name)
		if _, hasMasterRoleLabel := n.Labels[LabelNodeRoleMaster]; !hasMasterRoleLabel {
			// n has non-master now
			//fmt.Printf("[%s] is not Master node\n", n.Name)
			for _, condition := range n.Status.Conditions {
				if condition.Type == v1.NodeReady && condition.Status == v1.ConditionTrue {
					fmt.Printf("[%s] is non-master, and ready node\n", n.Name)
					runNode = n.Name
					break Outerloop
				}
			}
		}
	}
	if len(runNode) <= 0 {
		fmt.Printf("no node as non-master and Ready, can't continue\n")
		return
	}

	nameSpace = "default"

	pod := newPod(runNode, GiveLabelsCommand)
	_, err := clientSet.Core().Pods(nameSpace).Create(pod)
	fmt.Printf("after GiveLabels pod create: err is [%+v]\n", err)

	// TODO: Pod creation call seems to return quickly even when
	// local docker image is missing, i.e. there will be delay during
	// which docker image is in "Creation" mode, forsing also pod to remain
	// in creation mode. If same node happened to have those labels before,
	// whole test goes through then, which is not correct.
	// Some ideas: 1. should make sure no labels exist before test (hm but that
	// should already be the case!)
	// 2. should have better control here over pod lifecycle phases,
	// so that we do following operations in synced mode

	err = clientSet.Core().Pods(nameSpace).Delete(pod.Name, &metav1.DeleteOptions{})
	fmt.Printf("after GiveLabels pod delete: err is [%+v]\n", err)

	fmt.Printf("fakelabel is expected as [%s]\n", labels.SelectorFromSet(labels.Set(fakeFeatureLabels)).String())
	options := metav1.ListOptions{LabelSelector: labels.SelectorFromSet(labels.Set(fakeFeatureLabels)).String()}
	//fmt.Printf("options is [%+v]\n", options)

	matchedNodes, err := clientSet.CoreV1().Nodes().List(options)
	fmt.Printf("matchednodes has [%d] items\n", len(matchedNodes.Items))

	for _, n := range matchedNodes.Items {
		fmt.Printf("matched node with fakelabels: [%s]\n", n.Name)
	}

	pod = newPod(runNode, ClearLabelsCommand)
	_, err = clientSet.Core().Pods(nameSpace).Create(pod)
	fmt.Printf("after ClearLabels pod create: err is [%+v]\n", err)

	// Devel mode: comment following 2 lines out to keep pod running for examination
	err = clientSet.Core().Pods(nameSpace).Delete(pod.Name, &metav1.DeleteOptions{})
	fmt.Printf("after ClearLabels pod delete: err is [%+v]\n", err)
}
