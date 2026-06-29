package service

import (
	"context"
)

type Service interface {
	Name() string
	Run(ctx context.Context) error
	Reload(ctx context.Context) error
}

type ServiceManager struct {
	services map[string]Service
}

func NewServiceManager() *ServiceManager {
	return &ServiceManager{services: make(map[string]Service)}
}

func (s *ServiceManager) Register(service Service) {
	s.services[service.Name()] = service
}

func (s *ServiceManager) ReloadAll(ctx context.Context) error {
	for _, service := range s.services {
		if err := service.Reload(ctx); err != nil {
			return err
		}
	}
	return nil
}
