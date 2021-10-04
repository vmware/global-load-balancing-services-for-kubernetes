/*
Copyright The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned/typed/amko/v1alpha1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeAmkoV1alpha1 struct {
	*testing.Fake
}

func (c *FakeAmkoV1alpha1) ClusterSets(namespace string) v1alpha1.ClusterSetInterface {
	return &FakeClusterSets{c, namespace}
}

func (c *FakeAmkoV1alpha1) GSLBConfigs(namespace string) v1alpha1.GSLBConfigInterface {
	return &FakeGSLBConfigs{c, namespace}
}

func (c *FakeAmkoV1alpha1) GSLBHostRules(namespace string) v1alpha1.GSLBHostRuleInterface {
	return &FakeGSLBHostRules{c, namespace}
}

func (c *FakeAmkoV1alpha1) MCIs(namespace string) v1alpha1.MCIInterface {
	return &FakeMCIs{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeAmkoV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}