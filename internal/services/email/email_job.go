package email

import (
	"context"
	"queuebuzz/internal/helpers"
	"queuebuzz/internal/log"
	"time"
)

type Task struct {
	To      string
	Subject string
	HTML    string
	Retries int
}

type Sender interface {
	SendImmediate(to, subject, html string) error
}

type Job struct {
	sender     Sender
	taskChan   chan Task
	maxRetries int
}

func NewJob(sender Sender) *Job {
	return &Job{
		sender:     sender,
		taskChan:   make(chan Task, 100),
		maxRetries: 3,
	}
}

func (j *Job) Start(ctx context.Context) {
	log.Info().Msg("Email background worker started")
	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("Email background worker shutting down")
			return
		case task := <-j.taskChan:
			go j.processTask(task)
		}
	}
}

func (j *Job) Dispatch(to, subject, html string) {
	select {
	case j.taskChan <- Task{To: to, Subject: subject, HTML: html}:
	default:
		log.Error().Str("to", helpers.DerefString(helpers.MaskEmail(&to))).Msg("Email task channel full, dropping email")
	}
}

func (j *Job) processTask(task Task) {
	err := j.sender.SendImmediate(task.To, task.Subject, task.HTML)
	if err != nil {
		if task.Retries < j.maxRetries {
			task.Retries++
			backoff := time.Duration(task.Retries*task.Retries) * time.Second
			log.Warn().
				Err(err).
				Str("to", helpers.DerefString(helpers.MaskEmail(&task.To))).
				Int("retry", task.Retries).
				Dur("backoff", backoff).
				Msg("Email delivery failed, retrying...")

			time.Sleep(backoff)
			j.taskChan <- task
		} else {
			log.Error().
				Err(err).
				Str("to", helpers.DerefString(helpers.MaskEmail(&task.To))).
				Int("retries", task.Retries).
				Msg("Email delivery failed after max retries")
		}
	}
}
