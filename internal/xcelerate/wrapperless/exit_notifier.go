package wrapperless

// ExitNotifier delivers process-exit events. The darwin implementation uses
// kqueue EVFILT_PROC | NOTE_EXIT for instant notification; non-darwin falls
// back to polling (see exit_notifier_poll.go).
type ExitNotifier interface {
	Watch(pid int) error
	Unwatch(pid int)
	Events() <-chan int
	Close()
}
