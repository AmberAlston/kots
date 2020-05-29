package kotsadm

import (
	"bytes"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/kotsadm/types"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

func getDistributionYAML(deployOptions types.DeployOptions) (map[string][]byte, error) {
	docs := map[string][]byte{}
	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)

	var statefulset bytes.Buffer
	if err := s.Encode(distributionStatefulset(deployOptions), &statefulset); err != nil {
		return nil, errors.Wrap(err, "failed to marshal distribution statefulset")
	}
	docs["distribution-statefulset.yaml"] = statefulset.Bytes()

	var service bytes.Buffer
	if err := s.Encode(distributionService(deployOptions), &service); err != nil {
		return nil, errors.Wrap(err, "failed to marshal distribution service")
	}
	docs["distribution-service.yaml"] = service.Bytes()

	var configmap bytes.Buffer
	if err := s.Encode(distributionConfigMap(deployOptions), &configmap); err != nil {
		return nil, errors.Wrap(err, "failed to marshal distribution configmap")
	}

	return docs, nil
}

func ensureDistribution(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	if err := ensureDistributionConfigmap(deployOptions, clientset); err != nil {
		return errors.Wrap(err, "faield to ensure distribution configmap")
	}

	if err := ensureDistributionStatefulset(deployOptions, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure distribution statefulset")
	}

	if err := ensureDistributionService(deployOptions, clientset); err != nil {
		return errors.Wrap(err, "failed to ensure distribution service")
	}

	return nil
}

func ensureDistributionConfigmap(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	_, err := clientset.CoreV1().ConfigMaps(deployOptions.Namespace).Get("kotsadm-registry-storage-config", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing configmap")
		}

		_, err := clientset.CoreV1().ConfigMaps(deployOptions.Namespace).Create(distributionConfigMap(deployOptions))
		if err != nil {
			return errors.Wrap(err, "failed to create distribution configmap")
		}
	}

	return nil
}

func ensureDistributionStatefulset(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	_, err := clientset.AppsV1().StatefulSets(deployOptions.Namespace).Get("kotsadm-registry-storage", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing statefulset")
		}

		_, err := clientset.AppsV1().StatefulSets(deployOptions.Namespace).Create(distributionStatefulset(deployOptions))
		if err != nil {
			return errors.Wrap(err, "failed to create distrtibution statefulset")
		}
	}

	return nil
}

func ensureDistributionService(deployOptions types.DeployOptions, clientset *kubernetes.Clientset) error {
	_, err := clientset.CoreV1().Services(deployOptions.Namespace).Get("kotsadm-minio", metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing service")
		}

		_, err := clientset.CoreV1().Services(deployOptions.Namespace).Create(distributionService(deployOptions))
		if err != nil {
			return errors.Wrap(err, "failed to create service")
		}
	}

	return nil
}
