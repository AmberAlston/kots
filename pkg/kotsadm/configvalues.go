package kotsadm

import (
	"bytes"

	"github.com/pkg/errors"
	"github.com/replicatedhq/kots/pkg/kotsadm/types"
	corev1 "k8s.io/api/core/v1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

func ensureConfigValuesSecret(deployOptions *types.DeployOptions, clientset *kubernetes.Clientset) error {
	existingSecret, err := getConfigValuesSecret(deployOptions.Namespace, clientset)
	if err != nil {
		return errors.Wrap(err, "failed to check for existing config values secret")
	}

	if existingSecret != nil {
		return nil
	}

	s := json.NewYAMLSerializer(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme)
	var b bytes.Buffer
	if err := s.Encode(deployOptions.ConfigValues, &b); err != nil {
		return errors.Wrap(err, "failed to encode config values")
	}

	_, err = clientset.CoreV1().Secrets(deployOptions.Namespace).Create(configValuesSecret(deployOptions.Namespace, b.String()))
	if err != nil {
		return errors.Wrap(err, "failed to create config values secret")
	}

	return nil
}

func getConfigValuesSecret(namespace string, clientset *kubernetes.Clientset) (*corev1.Secret, error) {
	configValuesSecret, err := clientset.CoreV1().Secrets(namespace).Get("kotsadm-default-configvalues", metav1.GetOptions{})
	if err != nil {
		if kuberneteserrors.IsNotFound(err) {
			return nil, nil
		}

		return nil, errors.Wrap(err, "failed to get config values secret from cluster")
	}

	return configValuesSecret, nil
}
