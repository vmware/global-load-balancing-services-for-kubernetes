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
	"context"

	v1alpha2 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha2"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeGlobalDeploymentPolicies implements GlobalDeploymentPolicyInterface
type FakeGlobalDeploymentPolicies struct {
	Fake *FakeAmkoV1alpha2
	ns   string
}

var globaldeploymentpoliciesResource = schema.GroupVersionResource{Group: "amko.vmware.com", Version: "v1alpha2", Resource: "globaldeploymentpolicies"}

var globaldeploymentpoliciesKind = schema.GroupVersionKind{Group: "amko.vmware.com", Version: "v1alpha2", Kind: "GlobalDeploymentPolicy"}

// Get takes name of the globalDeploymentPolicy, and returns the corresponding globalDeploymentPolicy object, and an error if there is any.
func (c *FakeGlobalDeploymentPolicies) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha2.GlobalDeploymentPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(globaldeploymentpoliciesResource, c.ns, name), &v1alpha2.GlobalDeploymentPolicy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.GlobalDeploymentPolicy), err
}

// List takes label and field selectors, and returns the list of GlobalDeploymentPolicies that match those selectors.
func (c *FakeGlobalDeploymentPolicies) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha2.GlobalDeploymentPolicyList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(globaldeploymentpoliciesResource, globaldeploymentpoliciesKind, c.ns, opts), &v1alpha2.GlobalDeploymentPolicyList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha2.GlobalDeploymentPolicyList{ListMeta: obj.(*v1alpha2.GlobalDeploymentPolicyList).ListMeta}
	for _, item := range obj.(*v1alpha2.GlobalDeploymentPolicyList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested globalDeploymentPolicies.
func (c *FakeGlobalDeploymentPolicies) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(globaldeploymentpoliciesResource, c.ns, opts))

}

// Create takes the representation of a globalDeploymentPolicy and creates it.  Returns the server's representation of the globalDeploymentPolicy, and an error, if there is any.
func (c *FakeGlobalDeploymentPolicies) Create(ctx context.Context, globalDeploymentPolicy *v1alpha2.GlobalDeploymentPolicy, opts v1.CreateOptions) (result *v1alpha2.GlobalDeploymentPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(globaldeploymentpoliciesResource, c.ns, globalDeploymentPolicy), &v1alpha2.GlobalDeploymentPolicy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.GlobalDeploymentPolicy), err
}

// Update takes the representation of a globalDeploymentPolicy and updates it. Returns the server's representation of the globalDeploymentPolicy, and an error, if there is any.
func (c *FakeGlobalDeploymentPolicies) Update(ctx context.Context, globalDeploymentPolicy *v1alpha2.GlobalDeploymentPolicy, opts v1.UpdateOptions) (result *v1alpha2.GlobalDeploymentPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(globaldeploymentpoliciesResource, c.ns, globalDeploymentPolicy), &v1alpha2.GlobalDeploymentPolicy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.GlobalDeploymentPolicy), err
}

// Delete takes name of the globalDeploymentPolicy and deletes it. Returns an error if one occurs.
func (c *FakeGlobalDeploymentPolicies) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(globaldeploymentpoliciesResource, c.ns, name), &v1alpha2.GlobalDeploymentPolicy{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeGlobalDeploymentPolicies) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(globaldeploymentpoliciesResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha2.GlobalDeploymentPolicyList{})
	return err
}

// Patch applies the patch and returns the patched globalDeploymentPolicy.
func (c *FakeGlobalDeploymentPolicies) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha2.GlobalDeploymentPolicy, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(globaldeploymentpoliciesResource, c.ns, name, pt, data, subresources...), &v1alpha2.GlobalDeploymentPolicy{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha2.GlobalDeploymentPolicy), err
}
