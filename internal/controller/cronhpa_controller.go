/*
Copyright 2024.

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

package controller

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	autoscalingv1 "github.com/zhengyansheng/api/v1"
)

// CronHPAReconciler reconciles a CronHPA object
type CronHPAReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	CronManager *CronManager
}

//+kubebuilder:rbac:groups=autoscaling.zhengyansheng.com,resources=cronhpas,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=autoscaling.zhengyansheng.com,resources=cronhpas/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=autoscaling.zhengyansheng.com,resources=cronhpas/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the CronHPA object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.0/pkg/reconcile
func (r *CronHPAReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	klog.Infof("Start to handle cronHPA %s in %s namespace", req.Name, req.Namespace)

	// TODO(user): your logic here
	instance := &autoscalingv1.CronHPA{}
	err := r.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 业务逻辑
	instanceDeepCopy := instance.DeepCopy()
	statusConditions := instance.Status.Conditions
	leftConditions := make([]autoscalingv1.Condition, 0)

	// 检查spec是否变化
	if checkJobChange(instance.Spec, instance.Status) {
		// 如果变化删除所有的job
		for _, condition := range statusConditions {
			if err := r.CronManager.delete(condition.JobId); err != nil {
				klog.Errorf("delete job %s failed, err: %v", condition.JobId, err)
			}
		}
		instanceDeepCopy.Status.ScaleTargetRef = instance.Spec.ScaleTargetRef
	} else {
		// 检查job是否过期
		for _, condition := range statusConditions {

			skip := false

			for _, job := range instance.Spec.Jobs {

				if condition.Name == job.Name {
					if condition.Schedule != job.Schedule || condition.TargetSize != job.TargetSize {
						if err := r.CronManager.delete(condition.JobId); err != nil {
							klog.Errorf("delete job %s failed, err: %v", condition.JobId, err)
						}
						continue
					}
					// 没有任何变化
					skip = true
				}
			}

			// 如果变化了，则删除job
			if !skip {
				if condition.JobId != "" {
					if err := r.CronManager.delete(condition.JobId); err != nil {
						klog.Errorf("delete job %s failed, err: %v", condition.JobId, err)
					}
				}
			} else {
				leftConditions = append(leftConditions, condition)
			}
		}
	}

	instanceDeepCopy.Status.Conditions = leftConditions
	leftConditionsMap := make(map[string]autoscalingv1.Condition)
	for _, condition := range leftConditions {
		leftConditionsMap[condition.Name] = condition
	}

	noNeedUpdateStatus := true

	for _, job := range instance.Spec.Jobs {
		// 创建 status.Conditions
		jobCondition := autoscalingv1.Condition{
			Name:          job.Name,
			RunOnce:       job.RunOnce,
			Schedule:      job.Schedule,
			TargetSize:    job.TargetSize,
			LastProbeTime: metav1.Now(),
		}

		cronJob, err := NewCronJobHPA(instance, job, r.CronManager.scaler, r.CronManager.mapper, r.Client)
		if err != nil {
			jobCondition.State = autoscalingv1.Failed
			jobCondition.Message = fmt.Sprintf("Failed to update cron hpa job %s, err %v", job.Name, err)
			klog.Errorf("Failed to update cron hpa job %s, err %v", job.Name, err)
		} else {

			if condition, ok := leftConditionsMap[job.Name]; ok {
				cronJob.SetID(condition.JobId)

				if condition.State == autoscalingv1.Succeed || condition.State == autoscalingv1.Failed {
					if err := r.CronManager.delete(condition.JobId); err != nil {
						klog.Errorf("delete job %s failed, err: %v", condition.JobId, err)
					}
					continue
				}
			}

			jobCondition.JobId = cronJob.ID()
			jobCondition.State = autoscalingv1.Submitted
			err := r.CronManager.createOrUpdate(cronJob)
			klog.Infof("createOrUpdate cronJob %s, err %v", job.Name, err)
			if err != nil {
				if errors.Is(err, NoNeedUpdate) {
					klog.Warningf("job %s no need update, continue", job.Name)
					continue
				}
				jobCondition.State = autoscalingv1.Failed
				jobCondition.Message = fmt.Sprintf("Failed to update cron hpa job %s, err %v", job.Name, err)
				klog.Errorf("Failed to update cron hpa job %s, err %v", job.Name, err)

			}
		}
		noNeedUpdateStatus = false
		instanceDeepCopy.Status.Conditions = updateCondition(instanceDeepCopy.Status.Conditions, jobCondition)
	}

	klog.Info(noNeedUpdateStatus, len(leftConditions), len(statusConditions))
	if !noNeedUpdateStatus || len(leftConditions) != len(statusConditions) {
		// 需要更新并且job condition的数量发生了变化
		klog.Infof("instance status %+v, name %v", instanceDeepCopy.Status.Conditions, instance.Name)
		if err := r.Update(ctx, instance); err != nil {
			klog.Errorf("Failed to update cron hpa %s in namespace %s, err %v", instance.Name, instance.Namespace, err)
			return ctrl.Result{}, err
		}
		klog.Infof("ScaleTargetRef>>> %+v", instanceDeepCopy.Status.ScaleTargetRef)
		klog.Infof("Conditions>>> %+v", len(instanceDeepCopy.Status.Conditions))
		if err := r.Status().Update(ctx, instanceDeepCopy); err != nil {
			klog.Errorf("Failed to update status cron hpa %s in namespace %s, err %v", instance.Name, instance.Namespace, err)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func updateCondition(conditions []autoscalingv1.Condition, jobCondition autoscalingv1.Condition) []autoscalingv1.Condition {
	r := make([]autoscalingv1.Condition, 0)

	m := make(map[string]autoscalingv1.Condition)
	for _, condition := range conditions {
		m[condition.Name] = condition
	}

	m[jobCondition.Name] = jobCondition
	for _, condition := range m {
		r = append(r, condition)
	}
	return r

}

// SetupWithManager sets up the controller with the Manager.
func (r *CronHPAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.CronHPA{}).
		Complete(r)
}
