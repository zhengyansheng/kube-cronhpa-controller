package controller

import (
	"github.com/pkg/errors"
	"github.com/ringtail/go-cron"
	autoscalingv1 "github.com/zhengyansheng/api/v1"
)

// checkJobChange checks if the job has changed return true, otherwise return false.
func checkJobChange(spec autoscalingv1.CronHPASpec, status autoscalingv1.CronHPAStatus) bool {
	if &status.ScaleTargetRef != nil && (spec.ScaleTargetRef.Kind != status.ScaleTargetRef.Kind || spec.ScaleTargetRef.Name != status.ScaleTargetRef.Name) {
		return true
	}
	return false
}

func checkPlanValid(plan string) error {
	_, err := cron.Parse(plan)
	return err
}

func checkRefValid(ref *TargetRef) error {
	if ref.RefVersion == "" || ref.RefGroup == "" || ref.RefName == "" || ref.RefNamespace == "" || ref.RefKind == "" {
		return errors.New("any properties in ref could not be empty")
	}
	return nil
}
