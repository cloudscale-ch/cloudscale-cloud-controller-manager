package testkit

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Service helps construct Kubernetes v1.Service objects for testing
type Service struct {
	Name string
}

func NewService(name string) *Service {
	return &Service{Name: name}
}

func (s *Service) V1() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: s.Name},
	}
}

func (s *Service) WithName(name string) *Service {
	s.Name = name
	return s
}
