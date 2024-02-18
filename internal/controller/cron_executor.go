package controller

import (
	"time"

	"github.com/ringtail/go-cron"
)

type FailedFindJobReason string

const (
	maxOutOfDateTimeout = time.Minute * 5
	JobTimeOut          = FailedFindJobReason("JobTimeOut")
)

type CronExecutor interface {
	Run()
	Stop()
	AddJob(job CronJob) error
	Update(job CronJob) error
	RemoveJob(job CronJob) error
	FindJob(job CronJob) (bool, FailedFindJobReason)
	ListEntries() []*cron.Entry
}

type cronHPAExecutor struct {
	Engine *cron.Cron
}

func (c cronHPAExecutor) Run() {
	c.Engine.Start()
}

func (c cronHPAExecutor) Stop() {
	c.Engine.Stop()
}

func (c cronHPAExecutor) AddJob(job CronJob) error {
	return c.Engine.AddJob(job.SchedulePlan(), job)
}

func (c cronHPAExecutor) Update(job CronJob) error {
	c.RemoveJob(job)
	return c.AddJob(job)
}

func (c cronHPAExecutor) RemoveJob(job CronJob) error {
	c.Engine.RemoveJob(job.ID())
	return nil
}

func (c cronHPAExecutor) FindJob(job CronJob) (bool, FailedFindJobReason) {
	for _, entry := range c.Engine.Entries() {
		if entry.Job.ID() == job.ID() {
			// clean up out of date jobs when it reached maxOutOfDateTimeout
			if entry.Next.Add(maxOutOfDateTimeout).After(time.Now()) {
				return true, ""
			}
			return false, JobTimeOut
		}
	}
	return false, ""
}

func (c cronHPAExecutor) ListEntries() []*cron.Entry {
	return c.Engine.Entries()
}

func NewCronHPAExecutor(tz *time.Location, handler func(job *cron.JobResult)) cronHPAExecutor {
	if tz == nil {
		// 获取当前时间的时区信息
		tz = time.Now().Location()
	}

	c := cronHPAExecutor{Engine: cron.NewWithLocation(tz)}
	c.Engine.AddResultHandler(handler)

	return c
}
