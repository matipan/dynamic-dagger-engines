// A module that guides users through the Dynamic Dagger Engines blog post: https://blog.matiaspan.dev/
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
)

const (
	ArgoVersion                = "v2.9.3"
	EksctlVersion              = "v0.169.0"
	KubectlVersion             = "v1.29.1"
	AWSIamAuthenticatorVersion = "https://github.com/kubernetes-sigs/aws-iam-authenticator/releases/download/v0.6.14/aws-iam-authenticator_0.6.14_linux_amd64"

	defaultCluster = `apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: dynamic-dagger-engines-guide
  region: us-east-2
  version: '1.28'
  tags:
    karpenter.sh/discovery: dynamic-dagger-engines-guide
 
iam:
  withOIDC: true # required by Karpenter

managedNodeGroups:
  - name: core-nodes
    amiFamily: Bottlerocket
    # https://github.com/awslabs/amazon-eks-ami/blob/e9f135ed7a1ec25c57dcd0e2aac8604f2c0eefbe/files/eni-max-pods.txt#L749
    instanceType: t3a.medium
    desiredCapacity: 2
    minSize: 1
    maxSize: 3
    volumeType: gp3
    ebsOptimized: true
    propagateASGTags: true

karpenter:
  version: 'v0.32.6'
  createServiceAccount: true 
  withSpotInterruptionQueue: true

addons:
  - name: aws-ebs-csi-driver
`
)

type Guide struct {
	AwsConfig  *File
	AwsCreds   *File
	AwsProfile string
}

func New(
	// +optional
	awsConfig *File,
	awsCreds *File,
	awsProfile string,
) *Guide {
	return &Guide{
		AwsCreds:   awsCreds,
		AwsProfile: awsProfile,
	}
}

func (m *Guide) Up(ctx context.Context,
	// +optional
	cluster *File,
) (string, error) {
	kubeconfig, err := m.CreateCluster(ctx, cluster)
	if err != nil {
		return "", err
	}

	if _, err := m.InstallArgo(ctx, kubeconfig); err != nil {
		return "", err
	}

	if _, err := m.InstallArgoGenerator(ctx, kubeconfig); err != nil {
		return "", err
	}

	return "ready to go! 🚀", nil
}

// Teardown can be used after the guide is complete to remove the cluster and
// all resources that were created for it. If you specified a custom cluster.yaml
// config file when setting everything up you should provide that same one here.
func (m *Guide) Teardown(ctx context.Context,
	// +optional
	cluster *File) (string, error) {
	if cluster == nil {
		cluster = dag.Container().
			WithNewFile("/cluster.yaml", ContainerWithNewFileOpts{
				Contents: defaultCluster,
			}).
			File("/cluster.yaml")
	}
	return m.eksctl(cluster).Delete(ctx)
}

// CreateCluster creates a minimal EKS cluster using eksctl. There is an optional
// `cluster.yaml` file you can specify to eksctl. If not the default cluster
// that gets created has a single managed node group with a maximum of two t3a.medium
// nodes. It returns the kubeconfig of the newly created cluster.
// TODO: clarify what infrastructure this creates and how much money it will cost
// per month.
func (m *Guide) CreateCluster(ctx context.Context,
	// +optional
	cluster *File,
) (*File, error) {
	// if no cluster config file was specified then we initialize with a default
	// one
	if cluster == nil {
		cluster = dag.Container().
			WithNewFile("/cluster.yaml", ContainerWithNewFileOpts{
				Contents: defaultCluster,
			}).
			File("/cluster.yaml")
	}
	eksctl := m.eksctl(cluster)

	if _, err := eksctl.Create(ctx); err != nil {
		return nil, err
	}

	return eksctl.Kubeconfig(), nil
}

// Installs ArgoCD on the provided EKS cluster.
func (m *Guide) InstallArgo(ctx context.Context, kubeconfig *File) (string, error) {
	kubectl := m.kubectl(kubeconfig)
	if _, err := kubectl.Exec(ctx, []string{"create", "namespace", "argocd"}); err != nil {
		return "", fmt.Errorf("failed to create argocd namespace: %s", err)
	}

	argoManifest := fmt.Sprintf("https://raw.githubusercontent.com/argoproj/argo-cd/%s/manifests/install.yaml", ArgoVersion)
	if _, err := kubectl.Exec(ctx, []string{"apply", "-n", "argocd", "-f", argoManifest}); err != nil {
		return "", fmt.Errorf("failed to install argocd: %s", err)
	}

	return "sucessfully installed ArgoCD", nil
}

// InstallArgoGenerator installs the argocd-github-release-generator on the
// kubernetes cluster.
func (m *Guide) InstallArgoGenerator(ctx context.Context, kubeconfig *File) (string, error) {
	dst := []byte{}
	base64.RawStdEncoding.Encode(dst, randStr(10))

	return m.kubectl(kubeconfig).Container().
		WithEnvVariable("ARGOCD_TOKEN", string(dst)).
		WithExec([]string{"sh", "-c", "envsubst < https://raw.githubusercontent.com/matipan/argocd-github-release-generator/v0.0.3/k8s/install.yaml | k apply -f -"}).
		Stdout(ctx)
}

func (m *Guide) eksctl(cluster *File) *Eksctl {
	return dag.Eksctl(m.AwsCreds, m.AwsProfile, cluster, EksctlOpts{Version: EksctlVersion})
}

func (m *Guide) kubectl(kubeconfig *File) *KubectlCli {
	opts := KubectlKubectlEksOpts{}
	if m.AwsConfig != nil {
		opts.AwsConfig = m.AwsConfig
	}
	return dag.Kubectl(kubeconfig).KubectlEks(m.AwsCreds, m.AwsProfile, opts)
}

func randStr(length uint) []byte {
	bytes := make([]byte, int(length))
	for i := uint(0); i < length; i++ {
		bytes[i] = byte('!' + rand.Intn('~'-'!'))
	}
	return bytes
}