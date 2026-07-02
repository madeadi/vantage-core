package main

import "time"

type DeliveryType string
type DeliveryStatus string

const (
	DeliveryTypeFromKitchen = "from_kitchen"
	DeliveryTypeToKitchen   = "to_kitchen"
)

type Delivery struct {
	ID          string
	Type        DeliveryType
	Phase       string
	IsPhaseDone bool
	Name        string
	Status      string

	FailureReason string

	UpdatedAt time.Time
	CreatedAt time.Time

	StartAt time.Time
	EndAt   time.Time
}
