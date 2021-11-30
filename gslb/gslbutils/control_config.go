/*
 * Copyright 2020-2021 VMware, Inc.
 * All Rights Reserved.
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*   http://www.apache.org/licenses/LICENSE-2.0
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*/

package gslbutils

import (
	gslbcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha1/clientset/versioned"
	gdpcs "github.com/vmware/global-load-balancing-services-for-kubernetes/internal/client/v1alpha2/clientset/versioned"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type amkoControlConfig struct {
	clientset     *kubernetes.Clientset
	gslbClientset *gslbcs.Clientset
	gdpClientset  *gdpcs.Clientset

	amkoPodObjectMeta *metav1.ObjectMeta

	amkoEventRecorder *EventRecorder

	publishGDPStatus   bool
	publishGSLBStatus  bool
	amkoCreatedByField string
}

var amkoControlConfigInstance *amkoControlConfig

func AMKOControlConfig() *amkoControlConfig {
	if amkoControlConfigInstance == nil {
		amkoControlConfigInstance = &amkoControlConfig{}
	}
	return amkoControlConfigInstance
}

func (c *amkoControlConfig) SetClientset(cs *kubernetes.Clientset) {
	c.clientset = cs
}

func (c *amkoControlConfig) Clientset() *kubernetes.Clientset {
	return c.clientset
}

func (c *amkoControlConfig) SetGSLBClientset(cs *gslbcs.Clientset) {
	c.gslbClientset = cs
}

func (c *amkoControlConfig) GSLBClientset() *gslbcs.Clientset {
	return c.gslbClientset
}

func (c *amkoControlConfig) SetGDPClientset(cs *gdpcs.Clientset) {
	c.gdpClientset = cs
}

func (c *amkoControlConfig) GDPClientset() *gdpcs.Clientset {
	return c.gdpClientset
}

func (c *amkoControlConfig) SetPublishGSLBStatus(val bool) {
	c.publishGSLBStatus = val
}

func (c *amkoControlConfig) PublishGSLBStatus() bool {
	return c.publishGSLBStatus
}

func (c *amkoControlConfig) SetPublishGDPStatus(val bool) {
	c.publishGDPStatus = val
}

func (c *amkoControlConfig) PublishGDPStatus() bool {
	return c.publishGDPStatus
}

func (c *amkoControlConfig) SetEventRecorder(id string, client kubernetes.Interface) {
	c.amkoEventRecorder = NewEventRecorder(id, client)
}

func (c *amkoControlConfig) EventRecorder() *EventRecorder {
	return c.amkoEventRecorder
}

func (c *amkoControlConfig) SaveAMKOPodObjectMeta(pod *v1.Pod) {
	c.amkoPodObjectMeta = &pod.ObjectMeta
}

func (c *amkoControlConfig) PodEventf(eventType, reason, message string, formatArgs ...string) {
	if c.amkoPodObjectMeta != nil {
		if len(formatArgs) > 0 {
			c.EventRecorder().Recorder.Eventf(&v1.Pod{ObjectMeta: *c.amkoPodObjectMeta}, eventType, reason, message, formatArgs)
		} else {
			c.EventRecorder().Recorder.Event(&v1.Pod{ObjectMeta: *c.amkoPodObjectMeta}, eventType, reason, message)
		}
	}
}

func (c *amkoControlConfig) SetCreatedByField(val string) {
	c.amkoCreatedByField = val
}

func (c *amkoControlConfig) CreatedByField() string {
	return c.amkoCreatedByField
}
