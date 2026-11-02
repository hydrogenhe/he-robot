package core

import (
	"context"
	"gpt-robot/model"
	"strings"
	"time"
)

type privatePushBinding struct {
	sender string
	peer   string
}

func (s *Service) privatePushBinding() *privatePushBinding {
	if !s.robotRunning() || s.PrivatePush == nil {
		return nil
	}
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	config := s.privateConfig
	if !config.Enabled || s.privateState.ReadPaused || !config.MemoryEnabled && (!config.AutoReply || s.privateState.Paused) {
		return nil
	}
	result := &privatePushBinding{
		sender: config.SenderUID,
		peer:   config.PeerUID,
	}
	return result
}
func (s *Service) wakePrivatePush() {
	select {
	case s.privatePushWake <- struct{}{}:
	default:
	}
}
func (s *Service) runPrivatePush(ctx context.Context) {
	var binding *privatePushBinding
	var cancel context.CancelFunc
	var done chan error
	var retry *time.Timer
	failures := 0
	defer s.disablePrivatePush()
	defer stopPrivatePushConnection(&cancel, &done, &retry)
	for {
		current := s.privatePushBinding()
		if current == nil || binding == nil || *current != *binding {
			stopPrivatePushConnection(&cancel, &done, &retry)
			binding = current
			failures = 0
			s.privateMu.Lock()
			s.privatePushStatus = model.PrivatePushStatus{Mode: "disabled"}
			s.privatePushKeys = map[string]bool{}
			s.privatePushPending = false
			s.privatePushPendingSource = ""
			s.privateDeferredSource = ""
			s.privateMu.Unlock()
		}
		if binding != nil && cancel == nil && retry == nil {
			cancel, done = s.startPrivatePushConnection(ctx, binding)
		}
		var delay <-chan time.Time
		if retry != nil {
			delay = retry.C
		}
		select {
		case <-ctx.Done():
			return
		case <-s.privatePushWake:
		case <-delay:
			retry = nil
		case err := <-done:
			cancel()
			cancel = nil
			done = nil
			s.privateMu.Lock()
			if s.privatePushStatus.Connected && time.Since(s.privatePushSince) >= time.Minute {
				failures = 0
			}
			failures++
			s.privatePushStatus = model.PrivatePushStatus{
				Mode:      "polling",
				ErrorCode: OperationErrorCode(err),
			}
			if s.privateState.ReadFailures == 0 && !s.privateState.ReadPaused {
				next := time.Now().Add(time.Duration(s.privateConfig.IntervalSeconds) * time.Second)
				if untilRun(s.privateState.NextAt) > time.Until(next) {
					s.privateState.NextAt = next.UTC().Format(time.RFC3339Nano)
				}
			}
			s.privateMu.Unlock()
			wait := min(300, 5*(1<<min(failures-1, 6)))
			retry = time.NewTimer(time.Duration(wait) * time.Second)
			s.wakeRobot()
		}
	}
}
func stopPrivatePushConnection(cancel *context.CancelFunc, done *chan error, retry **time.Timer) {
	if *cancel != nil {
		(*cancel)()
		<-*done
		*cancel = nil
		*done = nil
	}
	if *retry != nil {
		(*retry).Stop()
		*retry = nil
	}
}
func (s *Service) watchPrivatePush(ctx context.Context, binding *privatePushBinding, done chan<- error) {
	err := s.PrivatePush.WatchPrivate(ctx, binding.sender, binding.peer, func(event *model.PrivatePushEvent) { s.acceptPrivatePush(binding, event) })
	select {
	case done <- err:
	case <-ctx.Done():
		done <- ctx.Err()
	}
}
func (s *Service) acceptPrivatePush(binding *privatePushBinding, event *model.PrivatePushEvent) {
	if event == nil || !s.robotRunning() {
		return
	}
	s.privateMu.Lock()
	if s.privateConfig.SenderUID != binding.sender || s.privateConfig.PeerUID != binding.peer || !s.privateConfig.Enabled || s.privateState.ReadPaused {
		s.privateMu.Unlock()
		return
	}
	if event.Observed || event.Wake {
		s.privatePushStatus.Notifications++
		s.privatePushStatus.LastPath = event.Path
		s.privatePushStatus.LastMetadata = event.Metadata
		s.privatePushStatus.LastNotificationAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if event.Wake {
		s.privatePushStatus.Verified = true
		if event.GenericHint {
			s.privatePushStatus.Hints++
		} else {
			s.privatePushStatus.Matched++
		}
	}
	first := event.Connected && !s.privatePushStatus.Connected
	if first {
		s.privatePushSince = time.Now()
	}
	s.privatePushStatus.Connected = event.Connected
	if event.Connected {
		s.privatePushStatus.Mode = "push"
		s.privatePushStatus.ErrorCode = ""
	}
	if !first && !event.Wake {
		s.privateMu.Unlock()
		return
	}
	key := event.Key
	if event.GenericHint && !strings.HasPrefix(key, "notice:") {
		key = ""
	}
	if !first && key != "" && s.privatePushKeys[key] {
		s.privateMu.Unlock()
		return
	}
	if key != "" {
		if len(s.privatePushKeys) >= 128 {
			s.privatePushKeys = map[string]bool{}
		}
		s.privatePushKeys[key] = true
	}
	pending := s.privatePushPending
	if event.Wake && !event.GenericHint {
		s.privatePushPendingSource = "push"
	} else if event.Wake && s.privatePushPendingSource != "push" {
		s.privatePushPendingSource = "push-hint"
	} else if s.privatePushPendingSource == "" {
		s.privatePushPendingSource = "connect-sync"
	}
	s.privatePushPending = true
	s.privateMu.Unlock()
	if !pending {
		s.wakeRobot()
	}
}

func (s *Service) disablePrivatePush() {
	s.privateMu.Lock()
	defer s.privateMu.Unlock()
	s.privatePushStatus = model.PrivatePushStatus{Mode: "disabled"}
	s.privatePushPending = false
	s.privatePushPendingSource = ""
	s.privateDeferredSource = ""
}

func (s *Service) startPrivatePushConnection(ctx context.Context, binding *privatePushBinding) (context.CancelFunc, chan error) {
	s.privateMu.Lock()
	s.privatePushStatus.Mode = "connecting"
	s.privateMu.Unlock()
	run, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go s.watchPrivatePush(run, binding, done)
	return cancel, done
}
