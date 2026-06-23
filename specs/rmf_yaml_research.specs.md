# OpenRMF Config YAML Research

## Overview

OpenRMF uses two distinct categories of YAML files:

1. **Fleet Adapter `config.yaml`** — defines a fleet's physical properties, individual robots, API connection, and coordinate transforms. Written by hand.
2. **Building Map (`*.building.yaml`)** — the Traffic Editor project file. Encodes levels, vertices (waypoints), lanes, doors, lifts, and models. Generated/edited via the `rmf_traffic_editor` GUI, then **compiled** into nav_graph YAML files consumed by fleet adapters.

---

## 1. Fleet Adapter `config.yaml`

The primary robot/fleet configuration file with three top-level sections.

### Full Example (from `open-rmf/fleet_adapter_template`)

```yaml
# FLEET CONFIG =================================================================
rmf_fleet:
  name: "tinyRobot"

  # Kinematic limits: [max_velocity, max_acceleration]
  limits:
    linear: [0.5, 0.75]   # m/s, m/s^2
    angular: [0.6, 2.0]   # rad/s, rad/s^2

  # Robot footprint modeled as a circle
  profile:
    footprint: 0.3         # radius in meters (hard exclusion zone)
    vicinity: 0.5          # radius in meters (soft personal space)

  reversible: True         # can the robots drive backwards?

  # Battery model for energy-aware task planning
  battery_system:
    voltage: 12.0          # V
    capacity: 24.0         # Ahr
    charging_current: 5.0  # A

  # Physical parameters for drag/power estimation
  mechanical_system:
    mass: 20.0             # kg
    moment_of_inertia: 10.0  # kg*m^2
    friction_coefficient: 0.22

  # Baseline power draw (electronics, sensors, compute)
  ambient_system:
    power: 20.0            # W

  # Power draw of any onboard tool (manipulator, cleaner, etc.)
  tool_system:
    power: 0.0             # W

  # Battery thresholds
  recharge_threshold: 0.10  # don't dispatch tasks below 10% charge
  recharge_soc: 1.0         # charge to 100% when recharging

  publish_fleet_state: 10.0  # Hz — must match robot_state_update_frequency
  account_for_battery_drain: True

  # What tasks this fleet can perform
  task_capabilities:
    loop: True
    delivery: True
    clean: False

  # Custom performable actions (arbitrary strings your adapter handles)
  actions: ["some_action_here"]

  # What should the robot do after finishing a task?
  finishing_request: "park"  # options: park | charge | nothing

  # Responsive wait: robot actively avoids blocking other robots while waiting
  responsive_wait: True      # fleet-wide default

  # Individual robot entries — all robots must be listed here
  robots:
    tinyRobot1:
      charger: "tinyRobot1_charger"   # waypoint name from nav graph
      responsive_wait: False           # overrides fleet-wide setting
    tinyRobot2:
      charger: "tinyRobot2_charger"
      # omitting responsive_wait inherits the fleet-wide value

  robot_state_update_frequency: 10.0  # Hz

# FLEET MANAGER (robot API endpoint) ==========================================
fleet_manager:
  prefix: "http://127.0.0.1:8080"
  user: "some_user"
  password: "some_password"

# COORDINATE TRANSFORM ========================================================
# Maps RMF world coordinates to the robot's native coordinate frame.
# Minimum 4 matching point pairs per level for reliable estimation.
reference_coordinates:
  L1:
    rmf:   [[20.33, -3.156],
             [8.908, -2.57],
             [13.02, -3.601],
             [21.93, -4.124]]
    robot: [[59, 399],
             [57, 172],
             [68, 251],
             [75, 429]]
```

### Extended Robot Entry (Nav2 adapter, from `open-rmf/free_fleet`)

```yaml
  robots:
    nav2_tb3:
      charger: "tb3_charger"
      responsive_wait: False
      navigation_stack: 2          # 1 = Nav1, 2 = Nav2
      init_timeout_sec: 5
      initial_map: "L1"            # which level this robot starts on
      service_call_timeout_sec: 1.0
      map_frame: "map"
      robot_frame: "base_footprint"
      maps:
        L1:
          map_url: "/opt/ros/jazzy/share/nav2_bringup/maps/tb3_sandbox.yaml"
      # initial_pose: [x, y, yaw]  # optional: manually seed localization
```

### Clearpath / OTTO Variant (explicit transform matrix + per-robot rmf_config)

```yaml
rmf_fleet:
  name: "OTTO-1500"
  limits:
    linear: [2.0, 1.2]
    angular: [0.7, 0.7]
  profile:
    footprint: 0.5
    vicinity: 0.6
  reversible: False
  battery_system:
    voltage: 24.0
    capacity: 40.0
    charging_current: 26.4
  mechanical_system:
    mass: 80.0
    moment_of_inertia: 20.0
    friction_coefficient: 0.20
  ambient_system:
    power: 20.0
  tool_system:
    power: 50.0
  recharge_threshold: 0.30
  recharge_soc: 0.85
  publish_fleet_state: True
  account_for_battery_drain: True
  task_capabilities:
    loop: True
    delivery: True
    clean: False
    finishing_request: "nothing"

robots:
  robot_1:
    robot_config:
      filter_waypoints: True
      max_delay: 100000.0    # seconds before replanning
    rmf_config:
      start:
        map_name: "L1"       # must match a level name in the building map
      charger:
        waypoint: "charger_otto1500_1"   # named waypoint in nav graph
  robot_2:
    robot_config:
      filter_waypoints: True
      max_delay: 100000.0
    rmf_config:
      start:
        map_name: "L1"
      charger:
        waypoint: "charger_otto1500_2"
  robot_3:
    robot_config:
      filter_waypoints: True
      max_delay: 100000.0
    rmf_config:
      start:
        map_name: "L1"
      charger:
        waypoint: "charger_otto1500_3"

# Explicit affine transform matrices (alternative to reference_coordinates pairs)
rmf_to_robot_transformation:
  rotation: 0.0
  scale: 1.2504164209780637
  trans_x: -142.8728426330461
  trans_y: 123.66709935598917
robot_to_rmf_transformation:
  rotation: 0.0
  scale: 0.7997328493216024
  trans_x: 114.27146490650718
  trans_y: -98.8964868512317
```

---

## 2. Building Map: `*.building.yaml`

The Traffic Editor project file. Compiled with `building_map_generator nav` to produce per-fleet `nav_graph/0.yaml`, `nav_graph/1.yaml`, etc.

### Top-Level Structure

```yaml
coordinate_system: reference_image   # or "cartesian_meters"
name: my_building
crowd_sim:
  enable: 0
graphs: {}
levels:
  L1:
    elevation: 0
    drawing:
      filename: floorplan.png
    vertices: [...]
    lanes: [...]
    doors: [...]
    floors: [...]
    walls: {}
    measurements: [...]
    models: [...]
    layers: {}
    features: []
    constraints: []
  L2:
    elevation: 3.5
    ...
lifts:
  LiftA:
    x: 827
    y: 357.7
    yaw: 1.09
    depth: 2
    width: 2
    reference_floor_name: L1
    doors:
      door1:
        door_type: 2
        motion_axis_orientation: 1.57
        width: 1.0
        x: 1.0
        y: 0.0
    level_doors:
      L1: [door1]
      L2: [door1]
```

### Vertices (Waypoints)

Each vertex is an array: `[x, y, z, name, {params}]`

```yaml
vertices:
  - [1364.76, 1336.717, 0, "magni1_charger", {
      is_charger:       [4, true],
      is_parking_spot:  [4, true],
      dock_name:        [1, "dock_magni1"],
      spawn_robot_name: [1, "magni1"],
      spawn_robot_type: [1, "Magni"]
    }]
  - [900.0, 800.0, 0, "pantry", {
      is_holding_point: [4, true]
    }]
  - [500.0, 400.0, 0, ""]   # unnamed intermediate waypoint
```

**Param type encoding:**

| Type ID | Type   | Example value    |
|---------|--------|------------------|
| 1       | STRING | `[1, "dock_A"]`  |
| 2       | INT    | `[2, 0]`         |
| 3       | DOUBLE | `[3, 0.5]`       |
| 4       | BOOL   | `[4, true]`      |

**Common vertex params:**

| Param | Type | Meaning |
|---|---|---|
| `is_charger` | bool | This waypoint is a charging station |
| `is_parking_spot` | bool | Robot can idle/park here |
| `is_holding_point` | bool | RMF planner allows indefinite waits here |
| `dock_name` | string | Triggers `MODE_DOCKING` command as robot approaches |
| `spawn_robot_name` | string | Simulation: name of robot spawned here |
| `spawn_robot_type` | string | Simulation: model type to spawn |

### Lanes

Each lane: `[start_vertex_idx, end_vertex_idx, {params}]`

```yaml
lanes:
  - [39, 40, {
      bidirectional:       [4, true],
      graph_idx:           [2, 0],      # fleet index 0–8
      orientation:         [1, ""],     # "" | "forward" | "backward"
      speed_limit:         [3, 0.0],    # m/s; 0 = unlimited
      mutex:               [1, ""],     # mutex group for exclusive access
      demo_mock_floor_name: [1, ""],
      demo_mock_lift_name:  [1, ""]
    }]
```

**Key lane params:**

| Param | Type | Meaning |
|---|---|---|
| `bidirectional` | bool | Allow travel in both directions |
| `graph_idx` | int | Which fleet's nav graph (0–8) this lane belongs to |
| `orientation` | string | `"forward"` or `"backward"` constraint |
| `speed_limit` | double | Max speed on segment (m/s), 0 = no limit |
| `mutex` | string | Named mutex; only one robot enters at a time |

### Doors

```yaml
doors:
  - [31, 13, {
      name:             [1, "main_door"],
      type:             [1, "double_hinged"],
      motion_axis:      [1, "start"],
      motion_degrees:   [3, 90.0],
      motion_direction: [2, 1],
      right_left_ratio: [3, 1.0],
      plugin:           [1, "normal"]
    }]
```

### Measurements (scale calibration)

```yaml
measurements:
  - [1, 0, {distance: [3, 9.315]}]   # vertices 1 and 0 are 9.315 m apart
```

### Models (static scene objects)

```yaml
models:
  - {model_name: "OpenRobotics/Fridge", name: "Fridge", static: true,
     x: 1878.9, y: 679.6, yaw: 1.5708, z: 0, dispensable: false}
  - {model_name: "OpenRobotics/TeleportDispenser", name: "coke_dispenser",
     static: true, x: 2097.0, y: 649.0, yaw: 1.5708, z: 1.08, dispensable: false}
```

---

## 3. Compiled Nav Graph (`nav_graph/0.yaml`)

Generated by `building_map_generator nav` from `*.building.yaml`. Used directly by fleet adapters at runtime.

```yaml
vertices:
  - [x_meters, y_meters, {
      name: "tinyRobot1_charger",
      is_charger: true,
      is_parking_spot: true
    }]
  - [x_meters, y_meters, {name: "pantry", is_holding_point: true}]
  - [x_meters, y_meters, {name: ""}]

lanes:
  # bidirectional lanes from building.yaml are split into two unidirectional entries
  - [0, 1, {}]
  - [1, 0, {}]
  - [2, 3, {
      dock_name: "dock_sequence_A",
      door_name: "main_door",
      orientation_constraint: "forward",
      speed_limit: 0.5,
      mutex: "narrow_corridor"
    }]
  - [3, 2, {
      undock_name: "dock_sequence_A"
    }]
```

---

## 4. How the Pieces Relate

```
building.yaml (Traffic Editor project)
  └── levels[].vertices  ← named waypoints with attributes
  └── levels[].lanes     ← connectivity with graph_idx per fleet
  └── lifts, doors       ← infrastructure events
       ↓
  building_map_generator nav
       ↓
  nav_graph/0.yaml       ← fleet 0's graph (loaded by fleet adapter)
  nav_graph/1.yaml       ← fleet 1's graph
       ↑
  config.yaml (fleet adapter config)
    └── rmf_fleet.robots[].charger: "named_waypoint"  ← must match a vertex name in nav_graph
    └── rmf_fleet.robots[].rmf_config.start.map_name: "L1"  ← must match a level name
    └── reference_coordinates: { L1: { rmf: [...], robot: [...] } }
```

- The **`charger` waypoint name** in `config.yaml` must exactly match a vertex `name` in the compiled nav graph.
- The **`map_name`** / `initial_map` in `config.yaml` must match a `levels` key in `building.yaml`.
- **`graph_idx`** in lane params in `building.yaml` determines which fleet sees which lanes.
- The **coordinate transform** bridges the robot's local frame to RMF's world frame.

---

## 5. Key Field Reference

### `rmf_fleet` section

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Fleet identifier |
| `limits.linear` | [float, float] | yes | [max_vel m/s, max_accel m/s²] |
| `limits.angular` | [float, float] | yes | [max_vel rad/s, max_accel rad/s²] |
| `profile.footprint` | float | yes | Robot hard radius (m) |
| `profile.vicinity` | float | yes | Robot soft exclusion radius (m) |
| `reversible` | bool | yes | Can reverse direction |
| `battery_system.voltage` | float | yes | Nominal voltage (V) |
| `battery_system.capacity` | float | yes | Capacity (Ahr) |
| `battery_system.charging_current` | float | yes | Charge rate (A) |
| `mechanical_system.mass` | float | yes | Robot mass (kg) |
| `mechanical_system.moment_of_inertia` | float | yes | Rotational inertia (kg·m²) |
| `mechanical_system.friction_coefficient` | float | yes | Dimensionless |
| `ambient_system.power` | float | yes | Idle power draw (W) |
| `tool_system.power` | float | yes | Tool power draw (W) |
| `recharge_threshold` | float | yes | SOC below which robot refuses tasks |
| `recharge_soc` | float | yes | Target SOC when charging |
| `account_for_battery_drain` | bool | yes | Enable energy-aware planning |
| `task_capabilities.loop` | bool | yes | Can do loop/patrol tasks |
| `task_capabilities.delivery` | bool | yes | Can do delivery tasks |
| `task_capabilities.clean` | bool | no | Can do cleaning tasks |
| `finishing_request` | string | yes | `park` / `charge` / `nothing` |
| `responsive_wait` | bool | no | Fleet-wide responsive wait default |
| `actions` | [string] | no | Custom action identifiers |
| `publish_fleet_state` | float | yes | Hz for fleet state publishing |
| `robot_state_update_frequency` | float | yes | Hz for robot state polling |
| `robots` | map | yes | One entry per robot in fleet |

### Per-robot entry under `rmf_fleet.robots`

| Field | Type | Required | Description |
|---|---|---|---|
| `charger` | string | yes | Waypoint name of robot's charging station |
| `responsive_wait` | bool | no | Override fleet-wide setting |
| `navigation_stack` | int | adapter-specific | 1 = Nav1, 2 = Nav2 |
| `initial_map` | string | adapter-specific | Starting level name |
| `maps.<level>.map_url` | string | adapter-specific | Path to Nav2 map YAML |
| `initial_pose` | [x, y, yaw] | no | Seed pose for localization |

---

## Sources

- [fleet_adapter_template/config.yaml](https://github.com/open-rmf/fleet_adapter_template/blob/main/fleet_adapter_template/config.yaml)
- [fleet_adapter_clearpath/config.yaml](https://github.com/open-rmf/fleet_adapter_clearpath/blob/main/config.yaml)
- [rmf_demos fleet adapter config.yaml](https://github.com/open-rmf/rmf_demos/blob/main/rmf_demos_fleet_adapter/config.yaml)
- [free_fleet nav2 single robot config](https://github.com/open-rmf/free_fleet/blob/main/free_fleet_examples/config/fleet/nav2_tb3_simulation_fleet_config.yaml)
- [free_fleet nav2 multi-robot config](https://github.com/open-rmf/free_fleet/blob/main/free_fleet_examples/config/fleet/nav2_unique_multi_tb3_simulation_fleet_config.yaml)
- [rmf_demos office.building.yaml](https://github.com/open-rmf/rmf_demos/blob/main/rmf_demos_maps/maps/office/office.building.yaml)
- [Traffic Editor docs](https://osrf.github.io/ros2multirobotbook/traffic-editor.html)
- [Fleet Adapter Tutorial](https://osrf.github.io/ros2multirobotbook/integration_fleets_adapter_tutorial.html)
- [Mobile Robot Fleet Integration](https://osrf.github.io/ros2multirobotbook/integration_fleets.html)
