/*
 * Copyright 2019-2020 VMware, Inc.
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

package k8sobjects

import (
	"sync"
)

// Interface for k8s/openshift objects(e.g. route, service, ingress) with minimal information
type MetaObject interface {
	GetType() string
	GetName() string
	GetNamespace() string
	GetHostname() string
	GetIPAddr() string
	GetCluster() string
	UpdateHostMap(string)
	GetHostnameFromHostMap(string) string
	DeleteMapByKey(string)
	GetPaths() ([]string, error)
	GetPort() (int32, error)
	GetProtocol() (string, error)
	GetTLS() (bool, error)
	IsPassthrough() bool
	GetVirtualServiceUUID() string
	GetControllerUUID() string
}

type FilterableObject interface {
	ApplyFilter() bool
}

type IPHostname struct {
	IP       string
	Hostname string
}

// ObjHostMap stores a mapping between cluster+ns+objName to it's hostname
type ObjHostMap struct {
	HostMap map[string]IPHostname
	Lock    sync.Mutex
}

const (
	VSAnnotation         = "ako.vmware.com/host-fqdn-vs-uuid-map"
	ControllerAnnotation = "ako.vmware.com/controller-cluster-uuid"
)
