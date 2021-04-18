/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	minecraftv1alpha1 "github.com/sleyva/minecraft-operator/api/v1alpha1"
)

// WorldReconciler reconciles a World object
type WorldReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=minecraft.sleyva.io,resources=worlds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=minecraft.sleyva.io,resources=worlds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=minecraft.sleyva.io,resources=worlds/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the World object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *WorldReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Log.WithValues("world", req.NamespacedName)

	world := &minecraftv1alpha1.World{}
	if err := r.Get(ctx, req.NamespacedName, world); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	servicePortFinalizer := "minecraft.sleyva.io.svc.port"
	if world.ObjectMeta.DeletionTimestamp.IsZero() {
		if !containsString(world.ObjectMeta.Finalizers, servicePortFinalizer) {
			world.ObjectMeta.Finalizers = append(world.ObjectMeta.Finalizers, servicePortFinalizer)
			if err := r.Update(context.Background(), world); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if containsString(world.ObjectMeta.Finalizers, servicePortFinalizer) {
			// our finalizer is present, so lets handle any external dependency
			if err := r.removeSvcPort(world); err != nil {
				// if fail to delete the external dependency here, return with error
				// so that it can be retried
				if errors.IsNotFound(err) {
					return ctrl.Result{}, nil
				}
				return ctrl.Result{}, err
			}

			// remove our finalizer from the list and update it.
			world.ObjectMeta.Finalizers = removeString(world.ObjectMeta.Finalizers, servicePortFinalizer)
			if err := r.Update(context.Background(), world); err != nil {
				return ctrl.Result{}, err
			}
		}

		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: world.Name,
		},
	}

	if err := r.Get(ctx, types.NamespacedName{Name: namespace.Name}, &namespace); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new namespace", "name", namespace.Name)
			if err := r.Create(ctx, &namespace); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "Failed to create namespace", "Name", namespace.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	mcConfigmap := r.configmapForMinecraft(world)
	if err := r.Get(ctx, types.NamespacedName{Name: mcConfigmap.Name, Namespace: mcConfigmap.Namespace}, mcConfigmap); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating new configmap", "Name", mcConfigmap.Name)
			if err := r.Create(ctx, mcConfigmap); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "error creating configmap", "name", mcConfigmap.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	pvc := r.volumeClaimForMinecraft(world)
	if err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, pvc); err != nil {
		logger.Info("Creating a new PVC", "Deployment.Namespace", pvc.Namespace, "Deployment.Name", pvc.Name)
		if errors.IsNotFound(err) {
			if err := r.Create(ctx, pvc); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "failed to create pvc")
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	dep := r.deploymentForMinecraft(world, mcConfigmap, pvc)
	if err := r.Get(ctx, types.NamespacedName{Name: world.Name, Namespace: world.Name}, dep); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			if err := r.Create(ctx, dep); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
				return ctrl.Result{}, err
			}
			// Deployment created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	rconService := r.serviceForRCONMinecraft(world)
	if err := r.Get(ctx, types.NamespacedName{Name: rconService.Name, Namespace: rconService.Namespace}, rconService); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating a new Service", "Deployment.Namespace", rconService.Namespace, "Deployment.Name", rconService.Name)
			if err := r.Create(ctx, rconService); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "Failed to create new RCON Service", "Name", rconService.Name)
				return ctrl.Result{}, err
			}
			// Deployment created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		log.Error(err, "Failed to create new Service", "Deployment.Namespace", rconService.Namespace, "Deployment.Name", rconService.Name)
		return ctrl.Result{}, err
	}

	service := r.serviceForMinecraft(world)
	if err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, service); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating a new Server Service", "Name", service.Name)
			if err := r.Create(ctx, service); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "Failed to create new Service", "Deployment.Namespace", service.Namespace, "Deployment.Name", service.Name)
				return ctrl.Result{}, err
			}
			// Deployment created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	if err := r.ingressForMinecraft(world, ctx); err != nil {
		if !errors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *WorldReconciler) removeSvcPort(world *minecraftv1alpha1.World) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minecraft-lb-kong-proxy",
			Namespace: "default",
		},
	}
	if err := r.Get(context.Background(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, service); err != nil {
		return err
	}

	var portIndex int
	found := false
	for i, port := range service.Spec.Ports {
		if port.Name == world.Name {
			portIndex = i
			found = true
		}
	}

	if !found {
		return errors.NewNotFound(schema.GroupResource{Group: world.GroupVersionKind().Group, Resource: world.Kind}, world.Name)
	}

	patch := client.MergeFrom(service.DeepCopy())
	r.Log.Info("Removing service port", "World", world.Name, "Port", service.Spec.Ports[portIndex])
	r.Log.Info(fmt.Sprintf("Before: %v", service.Spec.Ports))
	service.Spec.Ports = remove(service.Spec.Ports, portIndex)
	r.Log.Info(fmt.Sprintf("After: %v", service.Spec.Ports))

	if err := r.Patch(context.Background(), service, patch); err != nil {
		if !errors.IsInvalid(err) {
			return err
		}
	}

	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: world.Name,
		},
	}

	if err := r.Delete(context.Background(), &namespace); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}

	return nil
}

func remove(s []corev1.ServicePort, i int) []corev1.ServicePort {
	s[len(s)-1], s[i] = s[i], s[len(s)-1]
	return s[:len(s)-1]
}

// Helper functions to check and remove string from a slice of strings.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func removeString(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func (r *WorldReconciler) ingressForMinecraft(m *minecraftv1alpha1.World, ctx context.Context) error {

	// Check if port exists
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minecraft-lb-kong-proxy",
			Namespace: "default",
		},
	}
	if err := r.Get(ctx, types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, service); err != nil {
		return err
	}
	for _, port := range service.Spec.Ports {
		if port.Name == m.Name {
			return errors.NewAlreadyExists(schema.GroupResource{Group: m.GroupVersionKind().Group, Resource: m.Kind}, m.Name)
		}
	}

	// Open port on service
	servicePatch := client.MergeFrom(service.DeepCopy())
	service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{Name: m.Name, Port: m.Spec.Port, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(int(m.Spec.Port))})
	r.Log.Info("Opening Service", "World", m.Name)
	if err := r.Patch(ctx, service, servicePatch); err != nil {
		if !errors.IsInvalid(err) {
			return err
		}
		return nil
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configuration.konghq.com/v1beta1",
			"kind":       "TCPIngress",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("%s-tcp-ingress", m.Name),
				"namespace": m.Name,
				"annotations": map[string]interface{}{
					"kubernetes.io/ingress.class": "kong",
				},
			},
			"spec": map[string]interface{}{
				"rules": []map[string]interface{}{
					{
						"port": m.Spec.Port,
						"backend": map[string]interface{}{
							"serviceName": fmt.Sprintf("%s-server", m.Name),
							"servicePort": 8080,
						},
					},
				},
			},
		},
	}

	if m.Spec.ColdStart {
		obj.Object["metadata"].(map[string]interface{})["annotations"].(map[string]interface{})["konghq.com/plugins"] = "minecraft"
	}

	gkv := obj.GroupVersionKind()
	mapping, err := r.Client.RESTMapper().RESTMapping(gkv.GroupKind(), gkv.Version)
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}

	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}

	// 2. Prepare the dynamic client
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return err
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		// namespaced resources should specify the namespace
		dr = dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		// for cluster-wide resources
		dr = dyn.Resource(mapping.Resource)
	}

	_, err = dr.Create(ctx, obj, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return err
		}
	}
	r.Log.Info("Created Ingress")

	return nil
}

func (r *WorldReconciler) volumeClaimForMinecraft(m *minecraftv1alpha1.World) *corev1.PersistentVolumeClaim {
	ls := labelsForMinecraft(m.Name)
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Name,
			Labels:    ls,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				"ReadWriteOnce",
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: *resource.NewQuantity(5*1000*1000*1000, resource.DecimalSI), // 5Gi
				},
			},
		},
	}
}

func (r *WorldReconciler) serviceForRCONMinecraft(m *minecraftv1alpha1.World) *corev1.Service {
	ls := labelsForMinecraft(m.Name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-rcon", m.Name),
			Namespace: m.Name,
			Labels:    ls,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Ports: []corev1.ServicePort{
				{
					Name:       "rcon",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(m.Spec.ServerProperties.RCONPort),
					TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: "rcon"},
				},
			},
		},
	}
}

func (r *WorldReconciler) serviceForMinecraft(m *minecraftv1alpha1.World) *corev1.Service {
	ls := labelsForMinecraft(m.Name)
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-server", m.Name),
			Namespace: m.Name,
			Labels:    ls,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "server",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(8080),
					TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: "minecraft"},
				},
			},
		},
	}
}

// deploymentForMemcached returns a memcached Deployment object
func (r *WorldReconciler) deploymentForMinecraft(m *minecraftv1alpha1.World, configmap *corev1.ConfigMap, volume *corev1.PersistentVolumeClaim) *appsv1.Deployment {

	var limit int
	switch playerCount := (m.Spec.ServerProperties.MaxPlayers); {
	case playerCount <= 10:
		limit = 4096 // 4GB
	case playerCount <= 20:
		limit = 8192 // 8GB
	default:
		limit = 14336 // 14GB
	}

	ls := labelsForMinecraft(m.Name)
	replicas := int32(1) // Can't load balance mc server
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:           fmt.Sprintf("sleyva97/%s-server:%s-alpine", m.Spec.ServerType, m.Spec.Version),
						ImagePullPolicy: corev1.PullAlways,
						Command: []string{
							"java",
							fmt.Sprintf("-Xmx%dM", limit),
							"-Xms512M",
							"-jar",
							"server.jar",
							"--universe", "/worlds/",
						},
						Name: "minecraft",
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								"memory": resource.MustParse(fmt.Sprintf("%dMi", limit)),
							},
							Requests: corev1.ResourceList{
								"memory": resource.MustParse("512Mi"),
							},
						},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: int32(8080),
								Name:          "minecraft",
							},
							{
								ContainerPort: int32(m.Spec.ServerProperties.RCONPort),
								Name:          "rcon",
							},
							{
								ContainerPort: int32(9999),
								Name:          "jmx",
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "world-volume",
								MountPath: "/worlds/",
							},
							{
								Name:      "server-properties",
								MountPath: "/game/server.properties",
								SubPath:   "server.properties",
							},
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "server-properties",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configmap.Name,
									},
								},
							},
						},
						{
							Name: "world-volume",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: volume.Name,
								},
							},
						},
					},
				},
			},
		},
	}
	// Set Memcached instance as the owner and controller
	ctrl.SetControllerReference(m, dep, r.Scheme)
	return dep
}

func (r *WorldReconciler) configmapForMinecraft(world *minecraftv1alpha1.World) *corev1.ConfigMap {
	ls := labelsForMinecraft(world.Name)
	m := world.Spec.ServerProperties
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      world.Name,
			Namespace: world.Name,
			Labels:    ls,
		},
		Data: map[string]string{
			"server.properties": fmt.Sprintf(`
#Minecraft server properties
enable-jmx-monitoring=%t
rcon.port=%d
level-seed=%s
gamemode=%s
enable-command-block=%t
enable-query=%t
generator-settings=%s
level-name=%s
motd=%s
query.port=%d
pvp=%t
generate-structures=%t
difficulty=%s
network-compression-threshold=%d
max-tick-time=%d
max-players=%d
use-native-transport=%t
online-mode=%t
enable-status=%t
allow-flight=%t
broadcast-rcon-to-ops=%t
view-distance=%d
max-build-height=%d
server-ip=%s
allow-nether=%t
server-port=8080
enable-rcon=%t
sync-chunk-writes=%t
op-permission-level=%d
prevent-proxy-connections=%t
resource-pack=%s
entity-broadcast-range-percentage=%d
rcon.password=%s
player-idle-timeout=%d
force-gamemode=%t
rate-limit=%d
hardcore=%t
white-list=%t
broadcast-console-to-ops=%t
spawn-npcs=%t
spawn-animals=%t
snooper-enabled=%t
function-permission-level=%d
level-type=%s
spawn-monsters=%t
enforce-whitelist=%t
resource-pack-sha1=%s
spawn-protection=%d
max-world-size=%d
			`,
				m.EnableJMXMonitoring,
				m.RCONPort,
				m.LevelSeed,
				m.Gamemode,
				m.EnableCommandBlock,
				m.EnableQuery,
				m.GeneratorSettings,
				m.LevelName,
				world.Name,
				m.QueryPort,
				m.PVP,
				m.GenerateStructures,
				m.Difficulty,
				m.NetworkCompressionThreshold,
				m.MaxTickTime,
				m.MaxPlayers,
				m.UseNativeTransport,
				m.OnlineMode,
				m.EnableStatus,
				m.AllowFlight,
				m.BroadcastRCONToOps,
				m.ViewDistance,
				m.MaxBuildHeight,
				m.ServerIP,
				m.AllowNether,
				m.EnableRCON,
				m.SyncChunkWrites,
				m.OpPermissionLevel,
				m.PreventProxyConnection,
				m.ResourcePack,
				m.EntityBroadcaseRangePercentage,
				m.RCONPassword,
				m.PlayerIdleTimeout,
				m.ForceGamemode,
				m.RateLimit,
				m.Hardcore,
				m.WhiteList,
				m.BroadcastConsoleToOps,
				m.SpawnNPCS,
				m.SpawnAnimals,
				m.SnooperEnabled,
				m.FunctionPermissionLevel,
				m.LevelType,
				m.SpawnMonster,
				m.EnforceWhitelist,
				m.ResourcePackSha1,
				m.SpawnProtection,
				m.MaxWorldSize,
			),
		},
	}
}

// labelsForMemcached returns the labels for selecting the resources
// belonging to the given memcached CR name.
func labelsForMinecraft(name string) map[string]string {
	return map[string]string{"app": "minecraft-server", "cr": name}
}

// getPodNames returns the pod names of the array of pods passed in
func getPodNames(pods []corev1.Pod) []string {
	var podNames []string
	for _, pod := range pods {
		podNames = append(podNames, pod.Name)
	}
	return podNames
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorldReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&minecraftv1alpha1.World{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
