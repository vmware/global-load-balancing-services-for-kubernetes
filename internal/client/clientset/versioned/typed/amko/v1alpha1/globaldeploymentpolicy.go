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

package v1alpha1

import (
	"time"

	v1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	scheme "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// GlobalDeploymentPoliciesGetter has a method to return a GlobalDeploymentPolicyInterface.
// A group's client should implement this interface.
type GlobalDeploymentPoliciesGetter interface {
	GlobalDeploymentPolicies(namespace string) GlobalDeploymentPolicyInterface
}

// GlobalDeploymentPolicyInterface has methods to work with GlobalDeploymentPolicy resources.
type GlobalDeploymentPolicyInterface interface {
	Create(*v1alpha1.GlobalDeploymentPolicy) (*v1alpha1.GlobalDeploymentPolicy, error)
	Update(*v1alpha1.GlobalDeploymentPolicy) (*v1alpha1.GlobalDeploymentPolicy, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*v1alpha1.GlobalDeploymentPolicy, error)
	List(opts v1.ListOptions) (*v1alpha1.GlobalDeploymentPolicyList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.GlobalDeploymentPolicy, err error)
	GlobalDeploymentPolicyExpansion
}

// globalDeploymentPolicies implements GlobalDeploymentPolicyInterface
type globalDeploymentPolicies struct {
	client rest.Interface
	ns     string
}

// newGlobalDeploymentPolicies returns a GlobalDeploymentPolicies
func newGlobalDeploymentPolicies(c *AmkoV1alpha1Client, namespace string) *globalDeploymentPolicies {
	return &globalDeploymentPolicies{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the globalDeploymentPolicy, and returns the corresponding globalDeploymentPolicy object, and an error if there is any.
func (c *globalDeploymentPolicies) Get(name string, options v1.GetOptions) (result *v1alpha1.GlobalDeploymentPolicy, err error) {
	result = &v1alpha1.GlobalDeploymentPolicy{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of GlobalDeploymentPolicies that match those selectors.
func (c *globalDeploymentPolicies) List(opts v1.ListOptions) (result *v1alpha1.GlobalDeploymentPolicyList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.GlobalDeploymentPolicyList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested globalDeploymentPolicies.
func (c *globalDeploymentPolicies) Watch(opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch()
}

// Create takes the representation of a globalDeploymentPolicy and creates it.  Returns the server's representation of the globalDeploymentPolicy, and an error, if there is any.
func (c *globalDeploymentPolicies) Create(globalDeploymentPolicy *v1alpha1.GlobalDeploymentPolicy) (result *v1alpha1.GlobalDeploymentPolicy, err error) {
	result = &v1alpha1.GlobalDeploymentPolicy{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		Body(globalDeploymentPolicy).
		Do().
		Into(result)
	return
}

// Update takes the representation of a globalDeploymentPolicy and updates it. Returns the server's representation of the globalDeploymentPolicy, and an error, if there is any.
func (c *globalDeploymentPolicies) Update(globalDeploymentPolicy *v1alpha1.GlobalDeploymentPolicy) (result *v1alpha1.GlobalDeploymentPolicy, err error) {
	result = &v1alpha1.GlobalDeploymentPolicy{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		Name(globalDeploymentPolicy.Name).
		Body(globalDeploymentPolicy).
		Do().
		Into(result)
	return
}

// Delete takes name of the globalDeploymentPolicy and deletes it. Returns an error if one occurs.
func (c *globalDeploymentPolicies) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *globalDeploymentPolicies) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	var timeout time.Duration
	if listOptions.TimeoutSeconds != nil {
		timeout = time.Duration(*listOptions.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Timeout(timeout).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched globalDeploymentPolicy.
func (c *globalDeploymentPolicies) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1alpha1.GlobalDeploymentPolicy, err error) {
	result = &v1alpha1.GlobalDeploymentPolicy{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("globaldeploymentpolicies").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
