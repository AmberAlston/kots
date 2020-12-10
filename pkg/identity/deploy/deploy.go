package deploy

import (
	"context"
	"crypto/md5"
	"fmt"
	"os"

	"github.com/pkg/errors"
	kotsv1beta1 "github.com/replicatedhq/kots/kotskinds/apis/kots/v1beta1"
	"github.com/replicatedhq/kots/pkg/ingress"
	kotsadmtypes "github.com/replicatedhq/kots/pkg/kotsadm/types"
	kotsadmversion "github.com/replicatedhq/kots/pkg/kotsadm/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

var (
	KotsIdentityLabelKey = "kots.io/identity"
)

func Deploy(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, dexConfig []byte, ingressSpec kotsv1beta1.IngressConfigSpec, registryOptions *kotsadmtypes.KotsadmOptions) error {
	if err := ensureSecret(ctx, clientset, namespace, namePrefix, dexConfig); err != nil {
		return errors.Wrap(err, "failed to ensure secret")
	}
	if err := ensureDeployment(ctx, clientset, namespace, namePrefix, dexConfig, registryOptions); err != nil {
		return errors.Wrap(err, "failed to ensure deployment")
	}
	if err := ensureService(ctx, clientset, namespace, namePrefix, ingressSpec); err != nil {
		return errors.Wrap(err, "failed to ensure service")
	}
	if err := ensureIngress(ctx, clientset, namespace, namePrefix, ingressSpec); err != nil {
		return errors.Wrap(err, "failed to ensure ingress")
	}
	return nil
}

func Configure(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, dexConfig []byte) error {
	if err := ensureSecret(ctx, clientset, namespace, namePrefix, dexConfig); err != nil {
		return errors.Wrap(err, "failed to ensure secret")
	}
	if err := patchDeploymentSecret(ctx, clientset, namespace, namePrefix, dexConfig); err != nil {
		return errors.Wrap(err, "failed to patch deployment secret")
	}
	return nil
}

func AdditionalLabels(namePrefix string) map[string]string {
	return map[string]string{
		KotsIdentityLabelKey: namePrefix,
	}
}

func ensureSecret(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, dexConfig []byte) error {
	secret := secretResource(namePrefix, dexConfig)

	existingSecret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secret.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing secret")
		}

		_, err = clientset.CoreV1().Secrets(namespace).Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create secret")
		}

		return nil
	}

	existingSecret = updateSecret(existingSecret, secret)

	_, err = clientset.CoreV1().Secrets(namespace).Update(ctx, existingSecret, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to update secret")
	}

	return nil
}

func secretResource(namePrefix string, dexConfig []byte) *corev1.Secret {
	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   prefixName(namePrefix, "dex"),
			Labels: kotsadmtypes.GetKotsadmLabels(AdditionalLabels(namePrefix)),
		},
		Data: map[string][]byte{
			"dexConfig.yaml": dexConfig,
		},
	}
}

func updateSecret(existingSecret, desiredSecret *corev1.Secret) *corev1.Secret {
	existingSecret.Data = desiredSecret.Data
	return existingSecret
}

func ensureDeployment(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, marshalledDexConfig []byte, registryOptions *kotsadmtypes.KotsadmOptions) error {
	deploymentName := prefixName(namePrefix, "dex")

	configChecksum := fmt.Sprintf("%x", md5.Sum(marshalledDexConfig))

	deployment := deploymentResource(deploymentName, configChecksum, namespace, registryOptions)

	existingDeployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing deployment")
		}

		_, err = clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create deployment")
		}

		return nil
	}

	existingDeployment = updateDeployment(namePrefix, existingDeployment, deployment)

	_, err = clientset.AppsV1().Deployments(namespace).Update(ctx, existingDeployment, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to update deployment")
	}

	return nil
}

func patchDeploymentSecret(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, marshalledDexConfig []byte) error {
	configChecksum := fmt.Sprintf("%x", md5.Sum(marshalledDexConfig))

	deployment := deploymentResource(prefixName(namePrefix, "dex"), configChecksum, namespace, nil)

	patch := fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kots.io/dex-secret-checksum":"%s"}}}}}`, deployment.Spec.Template.ObjectMeta.Annotations["kots.io/dex-secret-checksum"])

	_, err := clientset.AppsV1().Deployments(namespace).Patch(ctx, deployment.Name, k8stypes.StrategicMergePatchType, []byte(patch), metav1.PatchOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to patch deployment")
	}

	return nil
}

var (
	dexCPUResource    = resource.MustParse("100m")
	dexMemoryResource = resource.MustParse("50Mi")
)

func deploymentResource(namePrefix, configChecksum, namespace string, registryOptions *kotsadmtypes.KotsadmOptions) *appsv1.Deployment {
	replicas := int32(2)
	volume := configSecretVolume(namePrefix)

	image := "quay.io/dexidp/dex:v2.26.0"
	imagePullSecrets := []corev1.LocalObjectReference{}
	if registryOptions != nil {
		if s := kotsadmversion.KotsadmPullSecret(namespace, *registryOptions); s != nil {
			image = fmt.Sprintf("%s/dex:%s", kotsadmversion.KotsadmRegistry(*registryOptions), kotsadmversion.KotsadmTag(*registryOptions))
			imagePullSecrets = []corev1.LocalObjectReference{
				{
					Name: s.ObjectMeta.Name,
				},
			}
		}
	}

	env := []corev1.EnvVar{}
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy"} {
		if val := os.Getenv(name); val != "" {
			env = append(env, corev1.EnvVar{Name: name, Value: val})
		}
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   prefixName(namePrefix, "dex"),
			Labels: kotsadmtypes.GetKotsadmLabels(AdditionalLabels(namePrefix)),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": prefixName(namePrefix, "dex"),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": prefixName(namePrefix, "dex"),
					},
					Annotations: map[string]string{
						"kots.io/dex-secret-checksum": configChecksum,
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: imagePullSecrets,
					Containers: []corev1.Container{
						{
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Name:            "dex",
							Command:         []string{"/usr/local/bin/dex", "serve", "/etc/dex/cfg/dexConfig.yaml"},
							Ports: []corev1.ContainerPort{
								{Name: "http", ContainerPort: 5556},
							},
							Env: env,
							VolumeMounts: []corev1.VolumeMount{
								{Name: volume.Name, MountPath: "/etc/dex/cfg"},
							},
							Resources: corev1.ResourceRequirements{
								// Limits: corev1.ResourceList{
								// 	"cpu":    dexCPUResource,
								// 	"memory": dexMemoryResource,
								// },
								Requests: corev1.ResourceList{
									"cpu":    dexCPUResource,
									"memory": dexMemoryResource,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						volume,
					},
				},
			},
		},
	}
}

func configSecretVolume(namePrefix string) corev1.Volume {
	return corev1.Volume{
		Name: "config",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: prefixName(namePrefix, "dex"),
			},
		},
	}
}

func updateDeployment(namePrefix string, existingDeployment, desiredDeployment *appsv1.Deployment) *appsv1.Deployment {
	if len(existingDeployment.Spec.Template.Spec.Containers) == 0 {
		// wtf
		return desiredDeployment
	}

	if existingDeployment.Spec.Template.Annotations == nil {
		existingDeployment.Spec.Template.ObjectMeta.Annotations = map[string]string{}
	}
	existingDeployment.Spec.Template.ObjectMeta.Annotations["kots.io/dex-secret-checksum"] = desiredDeployment.Spec.Template.ObjectMeta.Annotations["kots.io/dex-secret-checksum"]

	existingDeployment.Spec.Template.Spec.Containers[0].Image = desiredDeployment.Spec.Template.Spec.Containers[0].Image

	existingDeployment = updateDeploymentConfigSecretVolume(namePrefix, existingDeployment, desiredDeployment)

	return existingDeployment
}

func updateDeploymentConfigSecretVolume(namePrefix string, existingDeployment *appsv1.Deployment, desiredDeployment *appsv1.Deployment) *appsv1.Deployment {
	if len(existingDeployment.Spec.Template.Spec.Containers) == 0 {
		return desiredDeployment
	}

	newConfigSecretVolume := configSecretVolume(namePrefix)
	newConfigSecretVolumeMount := corev1.VolumeMount{Name: newConfigSecretVolume.Name, MountPath: "/etc/dex/cfg"}

	var existingSecretVolumeName string
	for i, volumeMount := range existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if volumeMount.MountPath == "/etc/dex/cfg" {
			existingSecretVolumeName = volumeMount.Name
			existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts[i] = newConfigSecretVolumeMount
			break
		}
	}
	if existingSecretVolumeName != "" {
		for i, volume := range existingDeployment.Spec.Template.Spec.Volumes {
			if volume.Name == existingSecretVolumeName {
				existingDeployment.Spec.Template.Spec.Volumes[i] = newConfigSecretVolume
			}
		}
		return existingDeployment
	}

	existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts =
		append(existingDeployment.Spec.Template.Spec.Containers[0].VolumeMounts, newConfigSecretVolumeMount)
	existingDeployment.Spec.Template.Spec.Volumes =
		append(existingDeployment.Spec.Template.Spec.Volumes, newConfigSecretVolume)

	return existingDeployment
}

func ensureService(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, ingressSpec kotsv1beta1.IngressConfigSpec) error {
	service := serviceResource(namePrefix, ingressSpec)

	existingService, err := clientset.CoreV1().Services(namespace).Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		if !kuberneteserrors.IsNotFound(err) {
			return errors.Wrap(err, "failed to get existing service")
		}

		_, err = clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "failed to create service")
		}

		return nil
	}

	existingService = updateService(existingService, service)

	_, err = clientset.CoreV1().Services(namespace).Update(ctx, existingService, metav1.UpdateOptions{})
	if err != nil {
		return errors.Wrap(err, "failed to update service")
	}

	return nil
}

func serviceResource(namePrefix string, ingressSpec kotsv1beta1.IngressConfigSpec) *corev1.Service {
	serviceType := corev1.ServiceTypeClusterIP
	port := corev1.ServicePort{
		Name:       "http",
		Port:       5556,
		TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: 5556},
	}
	if ingressSpec.Enabled && ingressSpec.NodePort != nil && ingressSpec.NodePort.Port != 0 {
		port.NodePort = int32(ingressSpec.NodePort.Port)
		serviceType = corev1.ServiceTypeNodePort
	}
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   prefixName(namePrefix, "dex"),
			Labels: kotsadmtypes.GetKotsadmLabels(AdditionalLabels(namePrefix)),
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Selector: map[string]string{
				"app": prefixName(namePrefix, "dex"),
			},
			Ports: []corev1.ServicePort{
				port,
			},
		},
	}
}

func updateService(existingService, desiredService *corev1.Service) *corev1.Service {
	existingService.Spec.Ports = desiredService.Spec.Ports

	return existingService
}

func ensureIngress(ctx context.Context, clientset kubernetes.Interface, namespace string, namePrefix string, ingressSpec kotsv1beta1.IngressConfigSpec) error {
	if !ingressSpec.Enabled || ingressSpec.Ingress == nil {
		return deleteIngress(ctx, clientset, namespace, namePrefix)
	}
	dexIngress := ingressResource(namespace, namePrefix, *ingressSpec.Ingress)
	return ingress.EnsureIngress(ctx, clientset, namespace, dexIngress)
}

func ingressResource(namespace string, namePrefix string, ingressConfig kotsv1beta1.IngressResourceConfig) *extensionsv1beta1.Ingress {
	return ingress.IngressFromConfig(ingressConfig, prefixName(namePrefix, "dex"), prefixName(namePrefix, "dex"), 5556, AdditionalLabels(namePrefix))
}

func prefixName(prefix, name string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}
