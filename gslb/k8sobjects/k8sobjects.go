package k8sobjects

import (
	gdpv1alpha1 "amko/pkg/apis/avilb/v1alpha1"
)

// Interface for k8s/openshift objects(e.g. route, service, ingress) with minimal information
type MetaObject interface {
	GetType() string
	GetName() string
	GetNamespace() string

	SanityCheck(gdpv1alpha1.MatchRule) bool

	GlobOperate(gdpv1alpha1.MatchRule) bool
	EqualOperate(gdpv1alpha1.MatchRule) bool
	NotEqualOperate(gdpv1alpha1.MatchRule) bool
}
