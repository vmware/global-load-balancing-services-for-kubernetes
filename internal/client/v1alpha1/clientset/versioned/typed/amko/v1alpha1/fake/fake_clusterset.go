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

	v1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/apis/amko/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeClusterSets implements ClusterSetInterface
type FakeClusterSets struct {
	Fake *FakeAmkoV1alpha1
	ns   string
}

var clustersetsResource = schema.GroupVersionResource{Group: "amko.vmware.com", Version: "v1alpha1", Resource: "clustersets"}

var clustersetsKind = schema.GroupVersionKind{Group: "amko.vmware.com", Version: "v1alpha1", Kind: "ClusterSet"}

// Get takes name of the clusterSet, and returns the corresponding clusterSet object, and an error if there is any.
func (c *FakeClusterSets) Get(ctx context.Context, name string, options v1.GetOptions) (result *v1alpha1.ClusterSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(clustersetsResource, c.ns, name), &v1alpha1.ClusterSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ClusterSet), err
}

// List takes label and field selectors, and returns the list of ClusterSets that match those selectors.
func (c *FakeClusterSets) List(ctx context.Context, opts v1.ListOptions) (result *v1alpha1.ClusterSetList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(clustersetsResource, clustersetsKind, c.ns, opts), &v1alpha1.ClusterSetList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &v1alpha1.ClusterSetList{ListMeta: obj.(*v1alpha1.ClusterSetList).ListMeta}
	for _, item := range obj.(*v1alpha1.ClusterSetList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested clusterSets.
func (c *FakeClusterSets) Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(clustersetsResource, c.ns, opts))

}

// Create takes the representation of a clusterSet and creates it.  Returns the server's representation of the clusterSet, and an error, if there is any.
func (c *FakeClusterSets) Create(ctx context.Context, clusterSet *v1alpha1.ClusterSet, opts v1.CreateOptions) (result *v1alpha1.ClusterSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(clustersetsResource, c.ns, clusterSet), &v1alpha1.ClusterSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ClusterSet), err
}

// Update takes the representation of a clusterSet and updates it. Returns the server's representation of the clusterSet, and an error, if there is any.
func (c *FakeClusterSets) Update(ctx context.Context, clusterSet *v1alpha1.ClusterSet, opts v1.UpdateOptions) (result *v1alpha1.ClusterSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(clustersetsResource, c.ns, clusterSet), &v1alpha1.ClusterSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ClusterSet), err
}

// Delete takes name of the clusterSet and deletes it. Returns an error if one occurs.
func (c *FakeClusterSets) Delete(ctx context.Context, name string, opts v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(clustersetsResource, c.ns, name), &v1alpha1.ClusterSet{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeClusterSets) DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(clustersetsResource, c.ns, listOpts)

	_, err := c.Fake.Invokes(action, &v1alpha1.ClusterSetList{})
	return err
}

// Patch applies the patch and returns the patched clusterSet.
func (c *FakeClusterSets) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.ClusterSet, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(clustersetsResource, c.ns, name, pt, data, subresources...), &v1alpha1.ClusterSet{})

	if obj == nil {
		return nil, err
	}
	return obj.(*v1alpha1.ClusterSet), err
}
