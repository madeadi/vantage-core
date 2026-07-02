package main

import "vantageos-core/pkg/missionsdk"

type Config struct {
	Core        missionsdk.CoreServerConfig `yaml:"core_server"`
	Missions    []missionsdk.MissionConfig  `yaml:"missions"`
	Http        appConfig                   `yaml:"app"`
	FromKitchen FromKitchenConfig           `yaml:"from_kitchen"`
	ToKitchen   ToKitchenConfig             `yaml:"to_kitchen"`
}

type appConfig struct {
	Port string `yaml:"http_port"`
	Name string `yaml:"name"`
}

// FromKitchenConfig pins the physical agents used by the FROM_KITCHEN_V2
// mission handler. Mirrors the Java reference's constructor-injected
// VehicleAgent/GateAgent/RobotAgent/LiftAgent beans.
type FromKitchenConfig struct {
	VehicleID          string `yaml:"vehicle_id"`
	GateID             string `yaml:"gate_id"`
	KitchenRobotID     string `yaml:"kitchen_robot_id"`
	InstitutionRobotID string `yaml:"institution_robot_id"`
	LiftID             string `yaml:"lift_id"`
}

// ToKitchenConfig pins the physical agents used by the TO_KITCHEN mission
// handler. Same agent set as FromKitchenConfig since both flows use the
// same physical equipment.
type ToKitchenConfig struct {
	VehicleID          string `yaml:"vehicle_id"`
	GateID             string `yaml:"gate_id"`
	KitchenRobotID     string `yaml:"kitchen_robot_id"`
	InstitutionRobotID string `yaml:"institution_robot_id"`
	LiftID             string `yaml:"lift_id"`
}
