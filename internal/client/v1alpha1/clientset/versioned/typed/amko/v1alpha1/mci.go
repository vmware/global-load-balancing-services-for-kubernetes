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
	"context"
	"time"

	v1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	scheme "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// MCIsGetter has a method to return a MCIInterface.
// A group's client should implement this interface.
type MCIsGetter interface {
	MCIs(namespace string) MCIInterface
}

// MCIInterface has methods to work with MCI resources.
type MCIInterface interface {
	Create(ctx context.Context, mCI *v1alpha1.MCI, opts v1.CreateOptions) (*v1alpha1.MCI, error)
	Update(ctx context.Context, mCI *v1alpha1.MCI, opts v1.UpdateOptions) (*v1alpha1.MCI, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.MCI, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.MCIList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.MCI, err error)
	MCIExpansion
}

// mCIs implements MCIInterface
type mCIs struct {
	client rest.Interface
	ns     string
}

// newMCIs returns a MCIs
func newMCIs(c *AmkoV1alpha1Client, namespace string) *mCIs {
	return &mCIs{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the mCI, and returns the corresponding mCI object, and an error if there is any.
func (c *mCIs) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.MCI, err error) {
	result = &v1alpha1.MCI{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("mcis").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do(ctx).
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of MCIs that match those selectors.
func (c *mCIs) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.MCIList, err error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	result = &v1alpha1.MCIList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("mcis").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Do(ctx).
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested mCIs.
func (c *mCIs) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	var timeout time.Duration
	if opts.TimeoutSeconds != nil {
		timeout = time.Duration(*opts.TimeoutSeconds) * time.Second
	}
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("mcis").
		VersionedParams(&opts, scheme.ParameterCodec).
		Timeout(timeout).
		Watch(ctx)
}

// Create takes the representation of a mCI and creates it.  Returns the server's representation of the mCI, and an error, if there is any.
func (c *mCIs) Create(ctx context.Context, mCI *v1alpha1.MCI, opts v1.CreateOptions) (result *v1alpha1.MCI, err error) {
	result = &v1alpha1.MCI{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("mcis").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(mCI).
		Do(ctx).
		Into(result)
	return
}

// Update takes the representation of a mCI and updates it. Returns the server's representation of the mCI, and an error, if there is any.
func (c *mCIs) Update(ctx context.Context, mCI *v1alpha1.MCI, opts v1.UpdateOptions) (result *v1alpha1.MCI, err error) {
	result = &v1alpha1.MCI{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("mcis").
		Name(mCI.Name).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(mCI).
		Do(ctx).
		Into(result)
	return
}

// Delete takes name of the mCI and deletes it. Returns an error if one occurs.
func (c *mCIs) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("mcis").
		Name(name).
		Body(&opts).
		Do(ctx).
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *mCIs) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	var timeout time.Duration
	if listOpts.TimeoutSeconds != nil {
		timeout = time.Duration(*listOpts.TimeoutSeconds) * time.Second
	}
	return c.client.Delete().
		Namespace(c.ns).
		Resource("mcis").
		VersionedParams(&listOpts, scheme.ParameterCodec).
		Timeout(timeout).
		Body(&opts).
		Do(ctx).
		Error()
}

// Patch applies the patch and returns the patched mCI.
func (c *mCIs) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.MCI, err error) {
	result = &v1alpha1.MCI{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("mcis").
		Name(name).
		SubResource(subresources...).
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(data).
		Do(ctx).
		Into(result)
	return
}