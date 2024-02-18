package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ringtail/go-cron"
	autoscalingv1 "github.com/zhengyansheng/api/v1"
	scalelib "github.com/zhengyansheng/internal/lib"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/scale"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var ErrJobType = errors.New("job type error")
var NoNeedUpdate = errors.New("NoNeedUpdate")

const MaxRetryTimes = 3

type CronManager struct {
	cfg           *rest.Config
	client        client.Client
	jobQueue      *sync.Map
	cronExecutor  CronExecutor
	mapper        meta.RESTMapper
	scaler        scale.ScalesGetter
	eventRecorder record.EventRecorder
}

func (cm *CronManager) Run(stopCh <-chan struct{}) {
	cm.cronExecutor.Run()
	defer cm.cronExecutor.Stop()
	<-stopCh

}

func (cm *CronManager) createOrUpdate(job CronJob) error {
	value, ok := cm.jobQueue.Load(job.ID())
	if !ok {
		// if job not exist, create it
		if err := cm.cronExecutor.AddJob(job); err != nil {
			return err
		}

		// add job to queue
		cm.jobQueue.Store(job.ID(), job)

		return nil
	}

	j, ok := value.(*CronJobHPA)
	if !ok {
		return ErrJobType
	}
	if ok := j.Equals(job); !ok {
		// 如果job存在，但是job内容发生变化，更新job
		if err := cm.cronExecutor.Update(job); err != nil {
			return err
		}

		// add job to queue
		cm.jobQueue.Store(job.ID(), job)
		return nil

	}
	return NoNeedUpdate
}

func (cm *CronManager) JobResultHandler(j *cron.JobResult) {
	job := j.Ref.(*CronJobHPA)
	cronHpa := j.Ref.(*CronJobHPA).HPARef

	instance := &autoscalingv1.CronHPA{}
	err := cm.client.Get(context.TODO(), types.NamespacedName{Namespace: cronHpa.Namespace, Name: cronHpa.Name}, instance)
	if err != nil {
		klog.Errorf("Failed to fetch cronHPA job %s of cronHPA %s in namespace %s, err %v", job.Name(), cronHpa.Name, cronHpa.Namespace, err)
		return
	}
	deepCopy := instance.DeepCopy()

	condition := autoscalingv1.Condition{
		Name:          job.Name(),
		JobId:         job.ID(),
		RunOnce:       job.RunOnce,
		Schedule:      job.SchedulePlan(),
		TargetSize:    job.DesiredSize,
		LastProbeTime: metav1.Time{Time: time.Now()},
	}

	if j.Error != nil {
		condition.State = autoscalingv1.Failed
		condition.Message = fmt.Sprintf("cron hpa execute failed, err %v", j.Error)
	} else {
		condition.State = autoscalingv1.Succeed
		condition.Message = fmt.Sprintf("cron hpa job %s executed successfully", job.name)
	}

	var leftConditions []autoscalingv1.Condition
	for _, c := range deepCopy.Status.Conditions {
		if c.Name == job.Name() {
			leftConditions = append(leftConditions, condition)
		} else {
			leftConditions = append(leftConditions, c)
		}
	}
	deepCopy.Status.Conditions = leftConditions

	patchFailed := false
	for i := 0; i < MaxRetryTimes; i++ {
		if err := cm.client.Patch(context.Background(), instance, client.MergeFrom(deepCopy)); err != nil {
			klog.Infof("patch cronhpa %s err: %v", instance.Name, err)
			continue
		}
		if err := cm.client.Status().Update(context.Background(), deepCopy); err != nil {
			klog.Infof("update cronhpa status %s err: %v", instance.Name, err)
			continue
		}

		klog.Infof("patch cronhpa %s status successfully", instance.Name)
		patchFailed = true
		break
	}

	if !patchFailed {
		cm.eventRecorder.Event(instance, v1.EventTypeWarning, string(autoscalingv1.Failed), fmt.Sprintf("Failed to patch cron hpa err: %v", err))
		return
	}
	cm.eventRecorder.Event(instance, v1.EventTypeNormal, string(autoscalingv1.Succeed), "Patch cron hpa status successfully")
}

func (cm *CronManager) delete(id string) error {
	if value, ok := cm.jobQueue.Load(id); ok {
		// if job exist, delete it
		cm.jobQueue.Delete(id)

		// remove job from cron
		if err := cm.cronExecutor.RemoveJob(value.(CronJob)); err != nil {
			return err
		}
	}
	return nil
}

func NewCronManager(cfg *rest.Config, client client.Client, recorder record.EventRecorder) *CronManager {
	cm := &CronManager{
		cfg:           cfg,
		client:        client,
		jobQueue:      &sync.Map{},
		eventRecorder: recorder,
	}
	hpaClient := clientset.NewForConfigOrDie(cfg)

	apiGroupResources, err := restmapper.GetAPIGroupResources(hpaClient)
	if err != nil {
		klog.Fatalf("Failed to get api resources, because of %v", err)
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(apiGroupResources)

	// change the rest mapper to discovery resources
	scaleKindResolver := scale.NewDiscoveryScaleKindResolver(hpaClient.Discovery())

	scaleClient, err := scalelib.NewForConfig(cm.cfg, restMapper, dynamic.LegacyAPIPathResolverFunc, scaleKindResolver)
	if err != nil {
		klog.Fatalf("Failed to create scaler client,because of %v", err)
	}

	cm.mapper = restMapper
	cm.scaler = scaleClient
	cm.cronExecutor = NewCronHPAExecutor(nil, cm.JobResultHandler)

	return cm
}
