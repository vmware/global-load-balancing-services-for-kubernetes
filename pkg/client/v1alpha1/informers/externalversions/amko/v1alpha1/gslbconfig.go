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

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	amkov1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/apis/amko/v1alpha1"
	versioned "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/clientset/versioned"
	internalinterfaces "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/vmware/global-load-balancing-services-for-kubernetes/pkg/client/v1alpha1/listers/amko/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// GSLBConfigInformer provides access to a shared informer and lister for
// GSLBConfigs.
type GSLBConfigInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.GSLBConfigLister
}

type gSLBConfigInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewGSLBConfigInformer constructs a new informer for GSLBConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewGSLBConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredGSLBConfigInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredGSLBConfigInformer constructs a new informer for GSLBConfig type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredGSLBConfigInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AmkoV1alpha1().GSLBConfigs(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.AmkoV1alpha1().GSLBConfigs(namespace).Watch(context.TODO(), options)
			},
		},
		&amkov1alpha1.GSLBConfig{},
		resyncPeriod,
		indexers,
	)
}

func (f *gSLBConfigInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredGSLBConfigInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *gSLBConfigInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&amkov1alpha1.GSLBConfig{}, f.defaultInformer)
}

func (f *gSLBConfigInformer) Lister() v1alpha1.GSLBConfigLister {
	return v1alpha1.NewGSLBConfigLister(f.Informer().GetIndexer())
}
