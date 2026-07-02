package main

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type DeliveryService struct {
	mr DeliveryRepository
}

type DeliveryRepository interface {
	CreateDelivery(delivery *Delivery) error
	FindDeliveryByID(id string) (Delivery, error)
	LatestDelivery() ([]Delivery, error)
	UpdateDelivery(delivery Delivery) error
}

func NewSPSService(mr DeliveryRepository) *DeliveryService {
	return &DeliveryService{mr: mr}
}

func (s *DeliveryService) Create(missionType DeliveryType) (Delivery, error) {
	id, err := generateDeliveryID()
	if err != nil {
		return Delivery{}, err
	}

	d := Delivery{
		ID:        id,
		Type:      missionType,
		Status:    "created",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.mr.CreateDelivery(&d); err != nil {
		return Delivery{}, err
	}

	return d, nil
}

func generateDeliveryID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
