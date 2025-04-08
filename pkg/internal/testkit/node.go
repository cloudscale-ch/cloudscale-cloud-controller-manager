package testkit

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Node helps construct Kubernetes v1.Node objects for testing.
type Node struct {
	Name       string
	ProviderID string
}

func NewNode(name string) *Node {
	return &Node{Name: name}
}

func (n *Node) V1() *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: n.Name},
		Spec: v1.NodeSpec{
			ProviderID: n.ProviderID,
		},
	}
}

func (n *Node) WithProviderID(id string) *Node {
	n.ProviderID = id

	return n
}

func (n *Node) WithName(name string) *Node {
	n.Name = name

	return n
}
