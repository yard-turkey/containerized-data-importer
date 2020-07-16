/*
Copyright 2018 The CDI Authors.

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
	time "time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
	corev1alpha1 "kubevirt.io/containerized-data-importer/pkg/apis/core/v1alpha1"
	versioned "kubevirt.io/containerized-data-importer/pkg/client/clientset/versioned"
	internalinterfaces "kubevirt.io/containerized-data-importer/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "kubevirt.io/containerized-data-importer/pkg/client/listers/core/v1alpha1"
)

// DataVolumeInformer provides access to a shared informer and lister for
// DataVolumes.
type DataVolumeInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.DataVolumeLister
}

type dataVolumeInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewDataVolumeInformer constructs a new informer for DataVolume type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewDataVolumeInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredDataVolumeInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredDataVolumeInformer constructs a new informer for DataVolume type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredDataVolumeInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CdiV1alpha1().DataVolumes(namespace).List(options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.CdiV1alpha1().DataVolumes(namespace).Watch(options)
			},
		},
		&corev1alpha1.DataVolume{},
		resyncPeriod,
		indexers,
	)
}

func (f *dataVolumeInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredDataVolumeInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *dataVolumeInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&corev1alpha1.DataVolume{}, f.defaultInformer)
}

func (f *dataVolumeInformer) Lister() v1alpha1.DataVolumeLister {
	return v1alpha1.NewDataVolumeLister(f.Informer().GetIndexer())
}
