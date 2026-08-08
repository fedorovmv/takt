package application

import "takt/internal/notification"

type NotificationItem = notification.Item

// NotificationService owns notification use cases so transports do not create
// persistence-backed dispatchers directly.
type NotificationService struct{ Workspace string }

func (s *NotificationService) List(unreadOnly bool, limit int) ([]notification.Item, error) {
	return (notification.Dispatcher{Workspace: s.Workspace}).List(unreadOnly, limit)
}

func (s *NotificationService) Ack(id string) (*notification.Item, error) {
	return (notification.Dispatcher{Workspace: s.Workspace}).Ack(id)
}

func (s *NotificationService) Test(message string) (*notification.Item, error) {
	return (notification.Dispatcher{Workspace: s.Workspace}).Test(message)
}

func (s *NotificationService) Dispatch() ([]notification.Item, error) {
	return (notification.Dispatcher{Workspace: s.Workspace}).Dispatch()
}
