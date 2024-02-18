package main

import (
	"context"
	"crypto/tls"
	"os"
	"time"

	autoscalingv1 "github.com/zhengyansheng/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(autoscalingv1.AddToScheme(scheme))
}

func main() {
	config, err := clientcmd.BuildConfigFromFlags("", "/Users/zhengyansheng/.kube/self_test_cls")
	if err != nil {
		klog.Fatal(err)
	}

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   ":8089",
			SecureServing: false,
			TLSOpts:       []func(*tls.Config){},
		},
	})
	if err != nil {
		klog.Fatal(err)
	}
	r := mgr.GetClient()

	go func() {
		time.Sleep(3 * time.Second)
		key := client.ObjectKey{Name: "cronhpa-sample", Namespace: "default"}
		instance := &autoscalingv1.CronHPA{}

		err = r.Get(context.Background(), key, instance)
		if err != nil {
			klog.Fatal(err)
		}

		InstanceCopy := instance.DeepCopy()
		InstanceCopy.Status.ScaleTargetRef = instance.Spec.ScaleTargetRef

		if err := r.Update(context.Background(), InstanceCopy); err != nil {
			klog.Errorf("update instance err: %v", err.Error())
			return
		}
		err := r.Patch(context.Background(), instance, client.MergeFrom(InstanceCopy))
		if err != nil {
			klog.Errorf("patch instance err: %v", err.Error())
			return
		}

		klog.Info("goroutine finished")
	}()

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Fatal(err)
		os.Exit(1)
	}
	select {}
}
