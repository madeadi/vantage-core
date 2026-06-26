package agentskill

type MediaSkill interface {
	UploadMedia(filename string, data []byte) bool
	GetAllMedia() []RobotMedia
	GetMediaByFilename(filename string) RobotMedia
	DeleteMedia(filename string)
}
