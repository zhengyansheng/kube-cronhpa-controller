package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	uuid "github.com/satori/go.uuid"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	//"github.com/google/uuid"
	autoscalingv1 "github.com/zhengyansheng/api/v1"
	autoscalingapi "k8s.io/api/autoscaling/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	scaleclient "k8s.io/client-go/scale"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type CronJob interface {
	ID() string
	Name() string
	SetID(id string)
	Equals(Job CronJob) bool
	SchedulePlan() string
	Ref() *TargetRef
	CronHPAMeta() *autoscalingv1.CronHPA
	Run() error
}

type TargetRef struct {
	RefName      string
	RefNamespace string
	RefKind      string
	RefGroup     string
	RefVersion   string
}

// needed when compare equals.
func (tr *TargetRef) toString() string {
	return fmt.Sprintf("%s:%s:%s:%s:%s", tr.RefName, tr.RefNamespace, tr.RefKind, tr.RefGroup, tr.RefVersion)
}

type CronJobHPA struct {
	id           string
	name         string
	DesiredSize  int32
	Plan         string
	RunOnce      bool
	scaler       scaleclient.ScalesGetter
	mapper       apimeta.RESTMapper
	excludeDates []string
	client       client.Client
	TargetRef    *TargetRef
	HPARef       *autoscalingv1.CronHPA
}

func (c *CronJobHPA) ID() string {
	return c.id
}

func (c *CronJobHPA) Name() string {
	return c.name
}

func (c *CronJobHPA) SetID(id string) {
	c.id = id
}

func (c *CronJobHPA) Equals(Job CronJob) bool {
	// update will create a new uuid
	if c.id == Job.ID() && c.SchedulePlan() == Job.SchedulePlan() && c.Ref().toString() == Job.Ref().toString() {
		return true
	}
	return false
}

func (c *CronJobHPA) SchedulePlan() string {
	return c.Plan
}

func (c *CronJobHPA) Ref() *TargetRef {
	return c.TargetRef
}

func (c *CronJobHPA) CronHPAMeta() *autoscalingv1.CronHPA {
	return c.HPARef
}

func (c *CronJobHPA) Run() error {
	times := 0
	startTime := time.Now()

	for {
		// timeout and exit
		if startTime.Add(time.Second * 10).Before(time.Now()) {
			return fmt.Errorf("failed to scale %s %s in %s namespace to %d after retrying %d times and exit", c.TargetRef.RefKind, c.TargetRef.RefName, c.TargetRef.RefNamespace, c.DesiredSize, times)
		}

		if err := c.scheduledScaling(); err == nil {
			break
		}

		time.Sleep(time.Second * 3)
		times++
	}
	return nil
}

func (c *CronJobHPA) scheduledScaling() error {
	var scale *autoscalingapi.Scale
	var resource schema.GroupResource

	schemaGK := schema.GroupKind{Group: c.TargetRef.RefGroup, Kind: c.TargetRef.RefKind}
	mappings, err := c.mapper.RESTMappings(schemaGK)
	if err != nil {
		return fmt.Errorf("failed to create create mapping, because of %v", err)
	}

	found := false
	for _, mapping := range mappings {
		resource = mapping.Resource.GroupResource()
		scale, err = c.scaler.Scales(c.TargetRef.RefNamespace).Get(context.Background(), resource, c.TargetRef.RefName, v1.GetOptions{})
		if err == nil {
			found = true
			klog.Infof("%s %s in namespace %s has been scaled successfully. job: %s replicas: %d id: %s", c.TargetRef.RefKind, c.TargetRef.RefName, c.TargetRef.RefNamespace, c.Name(), c.DesiredSize, c.ID())
			break
		}
	}

	if !found {
		klog.Errorf("failed to find source target %s %s in %s namespace", c.TargetRef.RefKind, c.TargetRef.RefName, c.TargetRef.RefNamespace)
		return fmt.Errorf("failed to find source target %s %s in %s namespace", c.TargetRef.RefKind, c.TargetRef.RefName, c.TargetRef.RefNamespace)
	}

	scale.Spec.Replicas = c.DesiredSize
	// k scale --replicas=2 deployment/nginx-deployment-basic
	_, err = c.scaler.Scales(c.TargetRef.RefNamespace).Update(context.Background(), resource, scale, metav1.UpdateOptions{})
	return err
}

func NewCronJobHPA(instance *autoscalingv1.CronHPA, job autoscalingv1.Job, scaler scaleclient.ScalesGetter, mapper apimeta.RESTMapper, client client.Client) (CronJob, error) {
	ref := strings.Split(instance.Spec.ScaleTargetRef.ApiVersion, "/")

	targetRef := &TargetRef{
		RefName:      instance.Spec.ScaleTargetRef.Name,
		RefKind:      instance.Spec.ScaleTargetRef.Kind,
		RefNamespace: instance.Namespace,
		RefGroup:     ref[0],
		RefVersion:   ref[1],
	}

	if err := checkRefValid(targetRef); err != nil {
		return nil, err
	}

	if err := checkPlanValid(job.Schedule); err != nil {
		return nil, err
	}

	return &CronJobHPA{
		id:          uuid.Must(uuid.NewV4(), nil).String(),
		name:        job.Name,
		Plan:        job.Schedule,
		DesiredSize: job.TargetSize,
		RunOnce:     job.RunOnce,
		TargetRef:   targetRef,
		HPARef:      instance,
		scaler:      scaler,
		mapper:      mapper,
		client:      client,
	}, nil

}
