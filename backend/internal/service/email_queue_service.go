package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// Task type constants
const (
	TaskTypeVerifyCode    = "verify_code"
	TaskTypePasswordReset = "password_reset"
)

// EmailTask 邮件发送任务
type EmailTask struct {
	Email      string
	SiteName   string
	TaskType   string // "verify_code" or "password_reset"
	ResetURL   string // Only used for password_reset task type
	Locale     string // Optional Accept-Language locale hint
	VerifyCode string // Only used for verify_code: the code already stored in cache

	// result carries the delivery outcome back to a caller that is waiting for
	// it. nil means fire-and-forget. The channel is buffered so a worker never
	// blocks if the caller has already given up.
	result chan error
}

// verifyCodeDeliveryTimeout bounds how long a caller waits for the worker to
// report the real SMTP outcome. It stays under the frontend's 30s HTTP timeout
// so the caller sees our error instead of a client-side abort.
const verifyCodeDeliveryTimeout = 25 * time.Second

// EmailQueueService 异步邮件队列服务
type EmailQueueService struct {
	emailService *EmailService
	taskChan     chan EmailTask
	wg           sync.WaitGroup
	stopChan     chan struct{}
	workers      int
}

// NewEmailQueueService 创建邮件队列服务
func NewEmailQueueService(emailService *EmailService, workers int) *EmailQueueService {
	if workers <= 0 {
		workers = 3 // 默认3个工作协程
	}

	service := &EmailQueueService{
		emailService: emailService,
		taskChan:     make(chan EmailTask, 100), // 缓冲100个任务
		stopChan:     make(chan struct{}),
		workers:      workers,
	}

	// 启动工作协程
	service.start()

	return service
}

// start 启动工作协程
func (s *EmailQueueService) start() {
	for i := 0; i < s.workers; i++ {
		s.wg.Add(1)
		go s.worker(i)
	}
	logger.LegacyPrintf("service.email_queue", "[EmailQueue] Started %d workers", s.workers)
}

// worker 工作协程
func (s *EmailQueueService) worker(id int) {
	defer s.wg.Done()

	for {
		select {
		case task := <-s.taskChan:
			s.processTask(id, task)
		case <-s.stopChan:
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d stopping", id)
			return
		}
	}
}

// processTask 处理任务
func (s *EmailQueueService) processTask(workerID int, task EmailTask) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	switch task.TaskType {
	case TaskTypeVerifyCode:
		err := s.emailService.DeliverVerifyCode(ctx, task.Email, task.SiteName, task.Locale, task.VerifyCode)
		if err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send verify code to %s: %v", workerID, task.Email, err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent verify code to %s", workerID, task.Email)
		}
		// Report the true outcome to the waiting caller, if any. The channel is
		// buffered, so this never blocks even if the caller already timed out.
		if task.result != nil {
			task.result <- err
		}
	case TaskTypePasswordReset:
		if err := s.emailService.SendPasswordResetEmailWithCooldown(ctx, task.Email, task.SiteName, task.ResetURL, task.Locale); err != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d failed to send password reset to %s: %v", workerID, task.Email, err)
		} else {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d sent password reset to %s", workerID, task.Email)
		}
	default:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Worker %d unknown task type: %s", workerID, task.TaskType)
	}
}

// EnqueueVerifyCode reserves the cooldown slot and persists the code
// synchronously, then queues only the delivery step (fire-and-forget).
//
// The reservation must happen here (not inside the worker): if it ran in the
// worker, the HTTP request would already have returned "code sent" while a
// concurrent request stole the slot, and every caller would see 200 with no
// mail actually delivered. Doing it up front lets the loser get a truthful 429.
//
// Callers that must not report success before the mail is actually accepted by
// the SMTP server should use SendVerifyCodeAndWait instead.
func (s *EmailQueueService) EnqueueVerifyCode(email, siteName string, locale ...string) error {
	_, err := s.enqueueVerifyCode(email, siteName, false, locale...)
	return err
}

// SendVerifyCodeAndWait queues the delivery and blocks until the worker reports
// the real SMTP outcome. This is what keeps the API from answering
// "verification code sent" for a mail that was never delivered.
func (s *EmailQueueService) SendVerifyCodeAndWait(ctx context.Context, email, siteName string, locale ...string) error {
	result, err := s.enqueueVerifyCode(email, siteName, true, locale...)
	if err != nil {
		return err
	}
	select {
	case deliveryErr := <-result:
		return deliveryErr
	case <-ctx.Done():
		// The caller went away; the buffered channel still lets the worker
		// finish and release the cooldown without blocking.
		return ctx.Err()
	case <-time.After(verifyCodeDeliveryTimeout):
		s.emailService.releaseCooldown(context.Background(), email)
		return fmt.Errorf("verify code delivery timed out after %s", verifyCodeDeliveryTimeout)
	}
}

func (s *EmailQueueService) enqueueVerifyCode(email, siteName string, waitForResult bool, locale ...string) (chan error, error) {
	if s == nil || s.emailService == nil {
		return nil, ErrEmailNotConfigured
	}
	// The queued task carries the exact code that was reserved, so a delivery
	// retry can never mail a code that differs from what Redis currently holds.
	code, err := s.emailService.prepareVerifyCode(context.Background(), email)
	if err != nil {
		return nil, err
	}

	var result chan error
	if waitForResult {
		// Buffered: the worker can always finish without blocking, even if the
		// caller already returned (timeout or cancellation).
		result = make(chan error, 1)
	}

	task := EmailTask{
		Email:      email,
		SiteName:   siteName,
		TaskType:   TaskTypeVerifyCode,
		Locale:     firstEmailLocale(locale),
		VerifyCode: code,
		result:     result,
	}

	select {
	case s.taskChan <- task:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Enqueued verify code task for %s", email)
		return result, nil
	default:
		// Nothing will deliver this code: release the slot so the user can retry
		// immediately instead of waiting out a cooldown for a mail never sent.
		if releaseErr := s.emailService.cache.ReleaseVerificationCodeCooldown(context.Background(), email); releaseErr != nil {
			logger.LegacyPrintf("service.email_queue", "[EmailQueue] Failed to release cooldown for %s: %v", email, releaseErr)
		}
		return nil, fmt.Errorf("email queue is full")
	}
}

// EnqueuePasswordReset 将密码重置邮件任务加入队列
func (s *EmailQueueService) EnqueuePasswordReset(email, siteName, resetURL string, locale ...string) error {
	task := EmailTask{
		Email:    email,
		SiteName: siteName,
		TaskType: TaskTypePasswordReset,
		ResetURL: resetURL,
		Locale:   firstEmailLocale(locale),
	}

	select {
	case s.taskChan <- task:
		logger.LegacyPrintf("service.email_queue", "[EmailQueue] Enqueued password reset task for %s", email)
		return nil
	default:
		return fmt.Errorf("email queue is full")
	}
}

// Stop 停止队列服务
func (s *EmailQueueService) Stop() {
	close(s.stopChan)
	s.wg.Wait()
	logger.LegacyPrintf("service.email_queue", "%s", "[EmailQueue] All workers stopped")
}
