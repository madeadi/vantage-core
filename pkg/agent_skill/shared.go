package agentskill

type Result struct {
	Err    error
	Status Status
}

type Status int

const (
	Success Status = iota
	Failed
	Timeout
	Aborted
	Cancelled
)

type RobotPose struct {
	X   float64
	Y   float64
	Yaw float64
}

type Lift struct {
	ID string
}

type Floor struct {
	ID       string
	Building string
	Level    string
}

type RobotMedia struct {
	Filename string
	Data     []byte
}

type IdleMediaSequence struct {
	Items []string
}
