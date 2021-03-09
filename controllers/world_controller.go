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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	minecraftv1alpha1 "github.com/sleyva/minecraft-operator/api/v1alpha1"
)

// WorldReconciler reconciles a World object
type WorldReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=minecraft.sleyva.com,resources=worlds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=minecraft.sleyva.com,resources=worlds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=minecraft.sleyva.com,resources=worlds/finalizers,verbs=update

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
	_ = r.Log.WithValues("world", req.NamespacedName)

	world := &minecraftv1alpha1.World{}
	if err := r.Get(ctx, req.NamespacedName, world); err != nil {
		return ctrl.Result{}, err
	}

	found := &appsv1.Deployment{}
	if err := r.Get(ctx, types.NamespacedName{Name: world.Name, Namespace: world.Namespace}, found); err != nil {
		if errors.IsNotFound(err) {
			configmap := r.configmapForMinecraft(world)
			if err := r.Create(ctx, configmap); err != nil {
				log.Error(err, "Failed to create new configmap", "Configmap.Namespace", configmap.Namespace, "Configmap.Name", configmap.Name)
				return ctrl.Result{}, err
			}
			dep := r.deploymentForMinecraft(world, configmap)
			log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			if err := r.Create(ctx, dep); err != nil {
				log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
				return ctrl.Result{}, err
			}
			// Deployment created successfully - return and requeue
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	return ctrl.Result{Requeue: true}, nil
}

// deploymentForMemcached returns a memcached Deployment object
func (r *WorldReconciler) deploymentForMinecraft(m *minecraftv1alpha1.World, configmap *corev1.ConfigMap) *appsv1.Deployment {
	ls := labelsForMinecraft(m.Name)
	replicas := m.Spec.Size

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
						Image: fmt.Sprintf("quay.io/sleyva/minecraft-server:%s-alpine", m.Spec.Version),
						Name:  "minecraft",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 8080,
							Name:          "minecraft",
						}},
						VolumeMounts: []corev1.VolumeMount{
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
	m := world.Spec.ServerProperties
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      world.Name,
			Namespace: world.Namespace,
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
				m.LevelType,
				m.MOTD,
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
