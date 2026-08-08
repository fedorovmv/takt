package application

import "takt/internal/notification"

type NotificationItem = notification.Item

type NotificationService struct{ backend NotificationBackend }

func (s *NotificationService) List(unreadOnly bool, limit int) ([]notification.Item, error) {
	return s.backend.List(unreadOnly, limit)
}
func (s *NotificationService) Ack(id string) (*notification.Item, error) { return s.backend.Ack(id) }
func (s *NotificationService) Test(message string) (*notification.Item, error) {
	return s.backend.Test(message)
}
func (s *NotificationService) Dispatch() ([]notification.Item, error) { return s.backend.Dispatch() }
