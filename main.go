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

/*
TODO:

Finalizer for svc, ingress, pvc, pv, lb_port
*/

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/mediocregopher/radix/v3"
	uberzap "go.uber.org/zap"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.

	"k8s.io/client-go/dynamic"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	minecraftv1alpha1 "github.com/sleyva/minecraft-operator/api/v1alpha1"
	"github.com/sleyva/minecraft-operator/controllers"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(minecraftv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "12db0f71.sleyva.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ports_cache := controllers.NewPorts(make(map[int32]bool), 2025, 1025)

	if err = (&controllers.WorldReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("World"),
		Scheme: mgr.GetScheme(),
		Ports:  ports_cache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "World")
		os.Exit(1)
	}
	if err = (&controllers.WorldReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("World"),
		Scheme: mgr.GetScheme(),
		Ports:  ports_cache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "World")
		os.Exit(1)
	}
	if err = (&controllers.WorldReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("World"),
		Scheme: mgr.GetScheme(),
		Ports:  ports_cache,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "World")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("health", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("check", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	go func() {
		setupLog.Info("Setting up port cache")
		service := corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name:      "minecraft-lb-kong-proxy",
			Namespace: "default",
		}}
		for true {
			if err := mgr.GetClient().Get(context.Background(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, &service); err != nil {
				setupLog.Error(err, "error creating port cache")
				time.Sleep(3 * time.Second)
				continue
			}
			setupLog.Info("Found Service!")
			break
		}

		for _, port := range service.Spec.Ports {
			if err := ports_cache.NewPort(port.Port); err != nil {
				setupLog.Error(err, "Error writing to port cache")
			}
		}
		setupLog.Info("Created port cache", "cache", fmt.Sprintf("%v", ports_cache))

		if err := StartServer(mgr.GetClient()); err != nil {
			setupLog.Error(err, "Server shutdown")
		}
	}()

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

}

func StartServer(k8sClient client.Client) error {
	secret := "M4RImpfr7WtOSmqf9QBR4eEIplIlxiB3/cVcXFKH1wU="
	e := echo.New()
	// e.AutoTLSManager.HostPolicy = autocert.HostWhitelist("api.tonether.com")
	// e.AutoTLSManager.Cache = autocert.DirCache("./cache")
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.JWTWithConfig(middleware.JWTConfig{
		SigningKey: []byte(secret),
	}))

	logger, err := uberzap.NewProduction()
	if err != nil {
		panic("failed to init logger")
	}

	token := jwt.New(jwt.SigningMethodHS256)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		return err
	}
	logger.Info("Admin Token", uberzap.String("token", tokenString))
	client, err := radix.NewPool("tcp", "redis-master.default.svc.cluster.local:6379", 10) // or any other client
	if err != nil {
		return err
	}

	g := e.Group("/v1")
	handler := Routes(k8sClient, logger, client)
	g.PUT("/worlds/:name", handler.NewWorld)
	g.GET("/worlds/:name", handler.GetWorld)
	g.DELETE("/worlds/:name", handler.DeleteWorld)
	g.PUT("/worlds/:name/:size", handler.ScaleWorld)

	g.GET("/worlds", handler.GetWorlds)

	data, err := json.MarshalIndent(e.Routes(), "", "  ")
	if err != nil {
		return err
	}

	logger.Info("API Information", uberzap.String("routes", string(data)))

	return e.Start(":3000")
}

type Handler struct {
	k8sClient client.Client
	logger    *uberzap.Logger
	redis     radix.Client
}

type WorldRequest struct {
	Name             string                             `json:"-"`
	ColdStart        bool                               `json:"coldStart,omitempty"`
	ServerProperties minecraftv1alpha1.ServerProperties `json:"serverProperties,omitempty"`
}

type WorldResponse struct {
	Name     string `json:"name"`
	Status   bool   `json:"status"`
	EndPoint string `json:"endpoint"`
}

type ListWorldResp struct {
	Name string `json:"name"`
	Port int64  `json:"port"`
}

func Routes(k8sClient client.Client, logger *uberzap.Logger, client radix.Client) *Handler {
	handler := Handler{k8sClient, logger, client}
	return &handler
}

func (h *Handler) ScaleWorld(c echo.Context) error {
	name := c.Param("name")
	size, err := strconv.ParseInt(c.Param("size"), 10, 32)
	if err != nil {
		h.logger.Error("err parsing param", uberzap.Error(err))
		return echo.ErrInternalServerError
	}

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}

	if err := h.k8sClient.Get(context.Background(), types.NamespacedName{Name: name, Namespace: name}, &deployment); err != nil {
		if k8serrors.IsNotFound(err) {
			return echo.ErrNotFound
		}
	}

	go func() {

		replicas := int32(size)
		if replicas == 0 {
			time.Sleep(5 * time.Minute)
			var conn int
			if err := h.redis.Do(radix.Cmd(&conn, "GET", name)); err != nil {
				// If can't read redis leave world online
				h.logger.Error("err getting connections", uberzap.Error(err))
				return
			}
			if conn > 0 {
				h.logger.Info("Redis connections shutdown aborted")
				return
			}
			h.logger.Info("No redis connection proceeding with shutdown")
		}

		patch := client.MergeFrom(deployment.DeepCopy())
		deployment.Spec.Replicas = &replicas

		if err := h.k8sClient.Patch(context.Background(), &deployment, patch); err != nil {
			h.logger.Error("err patching deployment", uberzap.Error(err))
			return
		}

	}()

	return c.JSON(http.StatusOK, map[string]interface{}{"scaling scheduled": size})
}

func (h *Handler) GetWorlds(c echo.Context) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minecraft-lb-kong-proxy",
			Namespace: "default",
		},
	}

	if err := h.k8sClient.Get(context.Background(), types.NamespacedName{Name: service.Name, Namespace: service.Namespace}, service); err != nil {
		if k8serrors.IsNotFound(err) {
			return echo.ErrNotFound
		}
	}

	response := map[int32]corev1.ServicePort{}

	for _, port := range service.Spec.Ports {
		response[port.Port] = corev1.ServicePort{Name: port.Name, Port: port.Port}
	}

	return c.JSON(http.StatusOK, &response)
}

func (h *Handler) GetWorld(c echo.Context) error {
	name := c.Param("name")

	mcWorld := minecraftv1alpha1.World{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}

	if err := h.k8sClient.Get(context.Background(), types.NamespacedName{Name: mcWorld.Name, Namespace: mcWorld.Namespace}, &mcWorld); err != nil {
		if k8serrors.IsNotFound(err) {
			return echo.ErrNotFound
		}
		return err
	}

	namespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if err := h.k8sClient.Get(context.Background(), types.NamespacedName{Name: namespace.Name}, &namespace); err != nil {
		if k8serrors.IsNotFound(err) {
			return echo.ErrNotFound
		}
		return err
	}

	if namespace.Status.Phase == corev1.NamespaceTerminating {
		return c.JSON(http.StatusNotFound, map[string]string{"message": fmt.Sprintf("world %s is being deleted", name)})
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "configuration.konghq.com/v1beta1",
			"kind":       "TCPIngress",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("%s-tcp-ingress", mcWorld.Name),
				"namespace": mcWorld.Name,
			},
		},
	}

	gkv := obj.GroupVersionKind()
	mapping, err := h.k8sClient.RESTMapper().RESTMapping(gkv.GroupKind(), gkv.Version)
	if err != nil {
		return err
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

	tcpIngress, err := dr.Get(context.Background(), obj.GetName(), metav1.GetOptions{})
	if err != nil {
		h.logger.Error("err getting Minecraft Ingress", uberzap.Error(err))
		return echo.ErrInternalServerError
	}

	port := tcpIngress.Object["spec"].(map[string]interface{})["rules"].([]interface{})[0].(map[string]interface{})["port"].(int64)
	resp := WorldResponse{
		Name:     mcWorld.Name,
		Status:   mcWorld.Status.Ready,
		EndPoint: fmt.Sprintf("portal.tonether.com:%d", port),
	}

	return c.JSON(http.StatusOK, &resp)
}

func (h *Handler) DeleteWorld(c echo.Context) error {
	name := c.Param("name")

	world := minecraftv1alpha1.World{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
		},
	}
	if err := h.k8sClient.Delete(context.Background(), &world); err != nil {
		h.logger.Error("err deleting Minecraft World", uberzap.Error(err))
		return echo.ErrInternalServerError
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("world %s deleted", name)})
}

func (h *Handler) NewWorld(c echo.Context) error {
	name := c.Param("name")
	req := WorldRequest{Name: name}

	if err := c.Bind(&req); err != nil {
		return err
	}

	mcWorld := minecraftv1alpha1.World{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: "default",
		},
		Spec: minecraftv1alpha1.WorldSpec{
			ColdStart:        req.ColdStart,
			ServerProperties: &req.ServerProperties,
		},
	}

	// Make sure world does not exist
	if err := h.k8sClient.Get(context.Background(), types.NamespacedName{Name: mcWorld.Name, Namespace: mcWorld.Namespace}, &mcWorld); err != nil {
		if !k8serrors.IsNotFound(err) {
			h.logger.Error("err getting Minecraft World", uberzap.Error(err))
			return echo.ErrInternalServerError
		}
	} else {
		return c.String(http.StatusBadRequest, fmt.Sprintf("World already exists: %s", mcWorld.Name))
	}

	if err := h.k8sClient.Create(context.Background(), &mcWorld); err != nil {
		h.logger.Error("err creating Minecraft World", uberzap.Error(err))
		return echo.ErrInternalServerError
	}

	return c.JSON(http.StatusOK, map[string]string{"message": fmt.Sprintf("World %s Created", mcWorld.Name)})

}
