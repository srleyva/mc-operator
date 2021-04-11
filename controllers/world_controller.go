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
	"sync"

	"math/rand"

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
	Ports  *Ports
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
		return ctrl.Result{}, err
	}

	configmap := &corev1.ConfigMap{}
	if err := r.Get(ctx, types.NamespacedName{Name: world.Name, Namespace: world.Namespace}, configmap); err != nil {
		if errors.IsNotFound(err) {
			configmap = r.configmapForMinecraft(world)
			logger.Info("Creating new configmap")
			if err := r.Create(ctx, configmap); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				logger.Error(err, "Failed to create new Deployment", "Deployment.Namespace", configmap.Namespace, "Deployment.Name", configmap.Name)
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, nil
	}

	pvc := r.volumeClaimForMinecraft(world)
	if err := r.Get(ctx, types.NamespacedName{Name: world.Name, Namespace: world.Namespace}, pvc); err != nil {
		logger.Info("Creating a new PVC", "Deployment.Namespace", pvc.Namespace, "Deployment.Name", pvc.Name)
		if errors.IsNotFound(err) {
			if err := r.Create(ctx, pvc); err != nil {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{Requeue: true}, err
		}
		return ctrl.Result{}, err
	}

	dep := r.deploymentForMinecraft(world, configmap, pvc)
	if err := r.Get(ctx, types.NamespacedName{Name: world.Name, Namespace: world.Namespace}, dep); err != nil {
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
			rconService = r.serviceForRCONMinecraft(world)
			log.Info("Creating a new Service", "Deployment.Namespace", rconService.Namespace, "Deployment.Name", rconService.Name)
			if err := r.Create(ctx, rconService); err != nil {
				if errors.IsAlreadyExists(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				log.Error(err, "Failed to create new Service", "Deployment.Namespace", rconService.Namespace, "Deployment.Name", rconService.Name)
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
			service = r.serviceForMinecraft(world)
			log.Info("Creating a new Service", "Deployment.Namespace", service.Namespace, "Deployment.Name", service.Name)
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

	// TODO update logic
	if err := r.ingressForMinecraft(world, ctx); err != nil {
		if !errors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
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

	// Get port
	port, err := r.Ports.RandPort()
	if err != nil {
		return err
	}

	// Open port on service
	servicePatch := client.MergeFrom(service.DeepCopy())
	service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{Name: m.Name, Port: port, Protocol: corev1.ProtocolTCP, TargetPort: intstr.FromInt(int(port))})
	r.Log.Info("Opening Service", "World", m.Name)
	if err := r.Patch(ctx, service, servicePatch); err != nil {
		if !errors.IsInvalid(err) {
			return err
		}
		return nil
	}

	// Just opening multiple ports in batch ops
	// Add Env Config and Port to deployment
	// deployment := &appsv1.Deployment{
	// 	ObjectMeta: metav1.ObjectMeta{
	// 		Name:      "minecraft-lb-kong",
	// 		Namespace: "default",
	// 	},
	// }
	// if err := r.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: deployment.Namespace}, deployment); err != nil {
	// 	return err
	// }

	// patch := client.MergeFrom(deployment.DeepCopy())

	// containerIndex := 0
	// for i, c := range deployment.Spec.Template.Spec.Containers {
	// 	if c.Name == "proxy" {
	// 		containerIndex = i
	// 	}
	// }

	// envIndex := 0
	// for i, e := range deployment.Spec.Template.Spec.Containers[containerIndex].Env {
	// 	if e.Name == "KONG_STREAM_LISTEN" {
	// 		envIndex = i
	// 	}
	// }

	// if deployment.Spec.Template.Spec.Containers[containerIndex].Env[envIndex].Value == "off" {
	// 	deployment.Spec.Template.Spec.Containers[containerIndex].Env[envIndex].Value = fmt.Sprintf("0.0.0.0:%d", port)
	// } else {
	// 	deployment.Spec.Template.Spec.Containers[containerIndex].Env[envIndex].Value = fmt.Sprintf(
	// 		"%s, 0.0.0.0:%d",
	// 		deployment.Spec.Template.Spec.Containers[containerIndex].Env[envIndex].Value,
	// 		port,
	// 	)
	// }

	// r.Log.Info("Patching Ingress Container", "World", m.Name)
	// if err := r.Client.Patch(ctx, deployment, patch); err != nil {
	// 	return err
	// }

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configuration.konghq.com/v1beta1",
			"kind":       "TCPIngress",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("%s-tcp-ingress", m.Name),
				"namespace": m.Namespace,
				"annotations": map[string]interface{}{
					"kubernetes.io/ingress.class": "kong",
				},
			},
			"spec": map[string]interface{}{
				"rules": []map[string]interface{}{
					{
						"port": port,
						"backend": map[string]interface{}{
							"serviceName": fmt.Sprintf("%s-server", m.Name),
							"servicePort": m.Spec.ServerProperties.ServerPort,
						},
					},
				},
			},
		},
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
			Namespace: m.Namespace,
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
			Namespace: m.Namespace,
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
			Namespace: m.Namespace,
			Labels:    ls,
		},
		Spec: corev1.ServiceSpec{
			Selector: ls,
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Name:       "server",
					Protocol:   corev1.ProtocolTCP,
					Port:       int32(m.Spec.ServerProperties.ServerPort),
					TargetPort: intstr.IntOrString{Type: intstr.String, StrVal: "minecraft"},
				},
			},
		},
	}
}

// deploymentForMemcached returns a memcached Deployment object
func (r *WorldReconciler) deploymentForMinecraft(m *minecraftv1alpha1.World, configmap *corev1.ConfigMap, volume *corev1.PersistentVolumeClaim) *appsv1.Deployment {
	ls := labelsForMinecraft(m.Name)
	replicas := int32(1) // Can't load balance mc server
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image:           fmt.Sprintf("sleyva97/paper-server:%s-alpine", m.Spec.Version),
						ImagePullPolicy: corev1.PullAlways,
						Command: []string{
							"java",
							"-Xmx1024M",
							"-Xms1024M",
							"-jar",
							"server.jar",
							"--nogui",
							"--noconsole",
							"-W", "/worlds/",
							"-c", "/etc/server.properties",
						},
						Name: "minecraft",
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: int32(m.Spec.ServerProperties.ServerPort),
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
								MountPath: "/etc/server.properties",
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
			Namespace: world.Namespace,
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
server-port=%d
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
				m.ServerPort,
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

type portInner map[int32]bool

type Ports struct {
	inner map[int32]bool
	sync.RWMutex
	min int32
	max int32
}

func NewPorts(ports map[int32]bool, max int32, min int32) *Ports {
	return &Ports{
		inner: ports,
		max:   max,
		min:   min,
	}
}

func (p *Ports) RandPort() (int32, error) {
	port := rand.Int31n(p.max-p.min) + p.min
	for p.Exists(port) {
		port = rand.Int31n(p.max-p.min) + p.min
	}
	if err := p.NewPort(port); err != nil {
		return 0, err
	}

	return port, nil
}

func (p *Ports) NewPort(port int32) error {
	p.Lock()
	defer p.Unlock()
	if _, ok := p.inner[port]; ok {
		return fmt.Errorf("Port already in use")
	}
	p.inner[port] = true
	return nil
}

func (p *Ports) Exists(port int32) bool {
	p.RLock()
	defer p.RUnlock()
	if _, ok := p.inner[port]; !ok {
		return false
	}
	return true
}
