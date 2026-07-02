package main

import (
	"errors"
	"sort"
	"sync"
)

type MemoryRepo struct {
	mu         sync.Mutex
	deliveries map[string]Delivery
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		deliveries: make(map[string]Delivery),
	}
}

var ErrDeliveryNotFound = errors.New("delivery not found")

func (m *MemoryRepo) CreateDelivery(delivery *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.deliveries[delivery.ID] = *delivery
	return nil
}

func (m *MemoryRepo) FindDeliveryByID(id string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delivery, ok := m.deliveries[id]
	if !ok {
		return Delivery{}, ErrDeliveryNotFound
	}

	return delivery, nil
}

func (m *MemoryRepo) UpdateDelivery(delivery Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.deliveries[delivery.ID]; !ok {
		return ErrDeliveryNotFound
	}

	m.deliveries[delivery.ID] = delivery
	return nil
}

// LatestDelivery returns the last 10 deliveries, most recently updated first.
func (m *MemoryRepo) LatestDelivery() ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	deliveries := make([]Delivery, 0, len(m.deliveries))
	for _, delivery := range m.deliveries {
		deliveries = append(deliveries, delivery)
	}

	sort.Slice(deliveries, func(i, j int) bool {
		return deliveries[i].UpdatedAt.After(deliveries[j].UpdatedAt)
	})

	if len(deliveries) > 10 {
		deliveries = deliveries[:10]
	}

	return deliveries, nil
}
